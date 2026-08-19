package gtm

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/nstranquist/ngtm/internal/lockfile"
)

func (s *TelemetryStore) insertLaunchUnlocked(ev LaunchEvent) error {
	raw, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT OR IGNORE INTO launch_events(ts, type, product, payload_json, content_sha256) VALUES(?,?,?,?,?)`,
		ev.TS, string(ev.Type), ev.Product, string(raw), contentSHA(raw))
	return err
}

// AppendLaunch validates, checks transitions, and writes in one locked transaction.
func (s *TelemetryStore) AppendLaunch(ev LaunchEvent) error {
	if err := ev.Validate(); err != nil {
		return fmt.Errorf("invalid launch event: %w", err)
	}
	raw, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	var inserted int64
	err = lockfile.WithFileLock(s.Path, func() error {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		history, err := readLaunchRows(tx)
		if err != nil {
			return err
		}
		if err := rejectWriteTransition(history, ev); err != nil {
			return fmt.Errorf("invalid launch event: %w", err)
		}
		res, err := tx.Exec(`INSERT OR IGNORE INTO launch_events(ts, type, product, payload_json, content_sha256) VALUES(?,?,?,?,?)`,
			ev.TS, string(ev.Type), ev.Product, string(raw), contentSHA(raw))
		if err != nil {
			return err
		}
		inserted, _ = res.RowsAffected()
		return tx.Commit()
	})
	if err != nil {
		return err
	}
	if inserted == 1 && !s.importing {
		_ = appendAndRotateJSONL(DefaultLaunchJSONLPath(), raw)
	}
	return nil
}

// ReadLaunch returns events in insert order. SQLite rows are written only after
// Validate, so this is fail-closed on decode errors.
func (s *TelemetryStore) ReadLaunch() (LaunchLedgerRead, error) {
	return readLaunchRowsReport(s.db)
}

type launchQuerier interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

func readLaunchRows(q launchQuerier) ([]LaunchEvent, error) {
	rep, err := readLaunchRowsReport(q)
	if err != nil {
		return nil, err
	}
	if len(rep.Issues) > 0 {
		return nil, &LaunchLedgerCorruptionError{Path: "telemetry.sqlite", Issues: rep.Issues}
	}
	return rep.Events, nil
}

func readLaunchRowsReport(q launchQuerier) (LaunchLedgerRead, error) {
	rows, err := q.Query(`SELECT id, payload_json FROM launch_events ORDER BY id`)
	if err != nil {
		return LaunchLedgerRead{}, err
	}
	defer rows.Close()
	var out LaunchLedgerRead
	for rows.Next() {
		var id int
		var payload string
		if err := rows.Scan(&id, &payload); err != nil {
			return LaunchLedgerRead{}, err
		}
		ev, err := decodeLaunchEventStrict([]byte(payload))
		if err != nil {
			out.Issues = append(out.Issues, LaunchLedgerIssue{Line: id, Code: "malformed_json", Message: err.Error()})
			continue
		}
		if err := ev.Validate(); err != nil {
			out.Issues = append(out.Issues, LaunchLedgerIssue{Line: id, Code: "invalid_event", Product: ev.Product, Message: err.Error()})
			continue
		}
		out.Events = append(out.Events, ev)
	}
	return out, rows.Err()
}
