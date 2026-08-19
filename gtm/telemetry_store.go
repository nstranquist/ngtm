package gtm

import (
	"bufio"
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nstranquist/ngtm/internal/lockfile"

	_ "modernc.org/sqlite"
)

const telemetrySchema = `
CREATE TABLE IF NOT EXISTS meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS runs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts TEXT NOT NULL,
  surface TEXT NOT NULL DEFAULT '',
  vertical TEXT NOT NULL DEFAULT '',
  subject TEXT NOT NULL DEFAULT '',
  provider TEXT NOT NULL DEFAULT '',
  verdict TEXT NOT NULL DEFAULT '',
  panel_median REAL,
  run_context TEXT NOT NULL DEFAULT '',
  tiers TEXT NOT NULL DEFAULT '',
  out_path TEXT NOT NULL DEFAULT '',
  metrics_json TEXT,
  extra_json TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS runs_ts ON runs(ts);
CREATE INDEX IF NOT EXISTS runs_vertical_ts ON runs(vertical, ts);
CREATE INDEX IF NOT EXISTS runs_subject ON runs(subject);
CREATE TABLE IF NOT EXISTS launch_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts TEXT NOT NULL,
  type TEXT NOT NULL,
  product TEXT NOT NULL,
  payload_json TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS launch_product_id ON launch_events(product, id);
CREATE INDEX IF NOT EXISTS launch_type ON launch_events(type);
`

// DefaultTelemetryDBPath is the operator SQLite ledger. NGTM_TELEMETRY_DB wins.
func DefaultTelemetryDBPath() string {
	if p := strings.TrimSpace(os.Getenv("NGTM_TELEMETRY_DB")); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "telemetry.sqlite"
	}
	return filepath.Join(home, ".nicos-dev", "gtm", "telemetry.sqlite")
}

// DefaultRunsJSONLPath is the live JSONL shadow (and import archive). SQLite is
// the query SoT; this file stays so `rg` still works. We do not delete it.
func DefaultRunsJSONLPath() string {
	if p := strings.TrimSpace(os.Getenv("NGTM_RUNS_TELEMETRY_PATH")); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "runs.jsonl"
	}
	return filepath.Join(home, ".nicos-dev", "gtm", "runs.jsonl")
}

// DefaultLaunchJSONLPath is the live launch-event JSONL shadow.
func DefaultLaunchJSONLPath() string {
	if p := strings.TrimSpace(os.Getenv("NGTM_LAUNCH_LEDGER")); p != "" && !IsSQLitePath(p) {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "launch-ledger.jsonl"
	}
	return filepath.Join(home, ".nicos-dev", "gtm", "launch-ledger.jsonl")
}

// IsSQLitePath reports whether path is a SQLite ledger (vs JSONL archive).
func IsSQLitePath(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".sqlite" || ext == ".sqlite3" || ext == ".db"
}

// TelemetryStore is the SQLite source of truth for intel runs and launch events.
type TelemetryStore struct {
	db        *sql.DB
	Path      string
	importing bool
}

// OpenTelemetryStore opens (or creates) the DB, applies schema, and imports
// leftover JSONL archives once.
func OpenTelemetryStore(path string) (*TelemetryStore, error) {
	if strings.TrimSpace(path) == "" {
		path = DefaultTelemetryDBPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && filepath.Dir(path) != "." {
		return nil, err
	}
	dsn := "file:" + path + "?_pragma=busy_timeout(8000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open telemetry db: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(telemetrySchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("telemetry schema: %w", err)
	}
	s := &TelemetryStore{db: db, Path: path}
	if s.getMeta("schema_version") == "" {
		if err := s.setMeta("schema_version", "1"); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	if err := s.applyMigrations(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("telemetry migrate: %w", err)
	}
	compressUncompressedRotations(DefaultRunsJSONLPath())
	compressUncompressedRotations(DefaultLaunchJSONLPath())
	if err := s.importJSONLIfNeeded(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func compressUncompressedRotations(live string) {
	for _, p := range jsonlFamily(live) {
		if p == live || strings.HasSuffix(p, ".gz") {
			continue
		}
		_ = gzipCompletedRotation(p)
	}
}

func (s *TelemetryStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *TelemetryStore) setMeta(k, v string) error {
	_, err := s.db.Exec(`INSERT INTO meta(key, value) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, k, v)
	return err
}

func (s *TelemetryStore) getMeta(k string) string {
	var v string
	if err := s.db.QueryRow(`SELECT value FROM meta WHERE key=?`, k).Scan(&v); err != nil {
		return ""
	}
	return v
}

// InsertRun records one intel/CLI run. Best-effort callers swallow the error.
func (s *TelemetryStore) InsertRun(fields map[string]any) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("telemetry store is closed")
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	ts := stringField(fields, "ts")
	if ts == "" {
		ts = time.Now().UTC().Format(time.RFC3339)
	}
	var metrics *string
	if m, ok := fields["metrics"]; ok && m != nil {
		b, err := json.Marshal(m)
		if err != nil {
			return err
		}
		s := string(b)
		metrics = &s
	}
	var median any
	if v, ok := fields["panel_median"]; ok {
		median = v
	}
	sha := contentSHA(raw)
	var inserted int64
	err = lockfile.WithFileLock(s.Path, func() error {
		res, err := s.db.Exec(`INSERT OR IGNORE INTO runs(
			ts, surface, vertical, subject, provider, verdict, panel_median,
			run_context, tiers, out_path, metrics_json, extra_json, content_sha256
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			ts,
			stringField(fields, "surface"),
			stringField(fields, "vertical"),
			stringField(fields, "subject"),
			stringField(fields, "provider"),
			stringField(fields, "verdict"),
			median,
			stringField(fields, "run_context"),
			stringField(fields, "tiers"),
			stringField(fields, "out"),
			metrics,
			string(raw),
			sha,
		)
		if err != nil {
			return err
		}
		inserted, _ = res.RowsAffected()
		return nil
	})
	if err != nil {
		return err
	}
	if inserted == 1 && !s.importing {
		_ = appendAndRotateJSONL(DefaultRunsJSONLPath(), raw)
	}
	return nil
}

func stringField(fields map[string]any, key string) string {
	if fields == nil {
		return ""
	}
	switch v := fields[key].(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		if v == nil {
			return ""
		}
		return fmt.Sprint(v)
	}
}

// TelemetryStatus is the doctor/status projection.
type TelemetryStatus struct {
	Path           string             `json:"path"`
	SchemaVersion  int                `json:"schema_version"`
	ImportedJSONL  bool               `json:"imported_jsonl"`
	ImportedAt     string             `json:"imported_at,omitempty"`
	Runs           int                `json:"runs"`
	LaunchEvents   int                `json:"launch_events"`
	FirstRun       string             `json:"first_run,omitempty"`
	LastRun        string             `json:"last_run,omitempty"`
	ByVertical     []VerticalRunStats `json:"by_vertical,omitempty"`
	LaunchByType   []NameCount        `json:"launch_by_type,omitempty"`
	JSONLArchive   string             `json:"jsonl_archive,omitempty"`
	LaunchArchive  string             `json:"launch_archive,omitempty"`
	JSONLShadow    bool               `json:"jsonl_shadow"`
	JSONLHotBytes  int64              `json:"jsonl_hot_bytes"`
	JSONLRotated   int                `json:"jsonl_rotated"`
}

// VerticalRunStats is one vertical's run count and mean panel score.
type VerticalRunStats struct {
	Vertical       string  `json:"vertical"`
	Runs           int     `json:"runs"`
	AvgPanelMedian float64 `json:"avg_panel_median,omitempty"`
}

// NameCount is a grouped counter.
type NameCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// Status returns counts and per-vertical averages.
func (s *TelemetryStore) Status() (TelemetryStatus, error) {
	st := TelemetryStatus{Path: s.Path, SchemaVersion: s.appliedSchema(), JSONLShadow: true}
	st.ImportedJSONL = s.getMeta("imported_jsonl") == "1"
	st.ImportedAt = s.getMeta("imported_at")
	st.JSONLArchive = DefaultRunsJSONLPath()
	st.JSONLHotBytes = jsonlHotBytes(st.JSONLArchive)
	if n := len(jsonlFamily(st.JSONLArchive)); n > 0 {
		st.JSONLRotated = n - 1
	}
	if p := strings.TrimSpace(os.Getenv("NGTM_LAUNCH_LEDGER")); p != "" && !IsSQLitePath(p) {
		st.LaunchArchive = p
	} else {
		home, err := os.UserHomeDir()
		if err == nil {
			st.LaunchArchive = filepath.Join(home, ".nicos-dev", "gtm", "launch-ledger.jsonl")
		}
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM runs`).Scan(&st.Runs); err != nil {
		return st, err
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM launch_events`).Scan(&st.LaunchEvents); err != nil {
		return st, err
	}
	var first, last sql.NullString
	_ = s.db.QueryRow(`SELECT MIN(ts), MAX(ts) FROM runs`).Scan(&first, &last)
	st.FirstRun, st.LastRun = first.String, last.String
	rows, err := s.db.Query(`SELECT vertical, COUNT(*), COALESCE(AVG(panel_median), 0)
		FROM runs GROUP BY vertical ORDER BY COUNT(*) DESC`)
	if err != nil {
		return st, err
	}
	defer rows.Close()
	for rows.Next() {
		var v VerticalRunStats
		if err := rows.Scan(&v.Vertical, &v.Runs, &v.AvgPanelMedian); err != nil {
			return st, err
		}
		st.ByVertical = append(st.ByVertical, v)
	}
	trows, err := s.db.Query(`SELECT type, COUNT(*) FROM launch_events GROUP BY type ORDER BY COUNT(*) DESC`)
	if err != nil {
		return st, err
	}
	defer trows.Close()
	for trows.Next() {
		var n NameCount
		if err := trows.Scan(&n.Name, &n.Count); err != nil {
			return st, err
		}
		st.LaunchByType = append(st.LaunchByType, n)
	}
	return st, rows.Err()
}

func (s *TelemetryStore) importJSONLIfNeeded() error {
	runsFP := jsonlFamilyFingerprint(DefaultRunsJSONLPath())
	launchFP := jsonlFamilyFingerprint(DefaultLaunchJSONLPath())
	if s.getMeta("imported_jsonl") == "1" &&
		s.getMeta("runs_jsonl_fp") == runsFP &&
		s.getMeta("launch_jsonl_fp") == launchFP {
		return nil
	}
	return s.ImportJSONL()
}

// ImportJSONL re-reads the JSONL shadows and inserts any rows whose content
// hash is not already in SQLite. Safe to run repeatedly. Does not delete JSONL.
func (s *TelemetryStore) ImportJSONL() error {
	s.importing = true
	defer func() { s.importing = false }()
	for _, p := range jsonlFamily(DefaultRunsJSONLPath()) {
		if err := s.importRunsJSONL(p); err != nil {
			return err
		}
	}
	for _, p := range jsonlFamily(DefaultLaunchJSONLPath()) {
		if err := s.importLaunchJSONL(p); err != nil {
			return err
		}
	}
	if err := s.setMeta("runs_jsonl_fp", jsonlFamilyFingerprint(DefaultRunsJSONLPath())); err != nil {
		return err
	}
	if err := s.setMeta("launch_jsonl_fp", jsonlFamilyFingerprint(DefaultLaunchJSONLPath())); err != nil {
		return err
	}
	if err := s.setMeta("imported_at", time.Now().UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	return s.setMeta("imported_jsonl", "1")
}

func (s *TelemetryStore) importRunsJSONL(path string) error {
	return foreachJSONL(path, func(raw []byte) error {
		var fields map[string]any
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil // skip corrupt archive rows
		}
		return s.InsertRun(fields)
	})
}

func (s *TelemetryStore) importLaunchJSONL(path string) error {
	return foreachJSONL(path, func(raw []byte) error {
		ev, err := decodeLaunchEventStrict(raw)
		if err != nil {
			return nil
		}
		if err := ev.Validate(); err != nil {
			return nil
		}
		return s.insertLaunchUnlocked(ev)
	})
}

func foreachJSONL(path string, fn func([]byte) error) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	var r io.Reader = f
	if strings.HasSuffix(path, ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		defer gz.Close()
		r = gz
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if err := fn([]byte(line)); err != nil {
			return err
		}
	}
	return sc.Err()
}

// ExportRunsJSONL writes every run's original blob as JSONL.
func (s *TelemetryStore) ExportRunsJSONL(w func([]byte) error) error {
	rows, err := s.db.Query(`SELECT extra_json FROM runs ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return err
		}
		if err := w([]byte(raw)); err != nil {
			return err
		}
	}
	return rows.Err()
}

// ExportLaunchJSONL writes every launch event payload as JSONL.
func (s *TelemetryStore) ExportLaunchJSONL(w func([]byte) error) error {
	rows, err := s.db.Query(`SELECT payload_json FROM launch_events ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return err
		}
		if err := w([]byte(raw)); err != nil {
			return err
		}
	}
	return rows.Err()
}

