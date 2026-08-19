package gtm

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

const telemetrySchemaCurrent = 2

func contentSHA(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (s *TelemetryStore) appliedSchema() int {
	v := strings.TrimSpace(s.getMeta("schema_version"))
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 1
	}
	return n
}

func (s *TelemetryStore) applyMigrations() error {
	cur := s.appliedSchema()
	if cur < 1 {
		cur = 1
	}
	if cur > telemetrySchemaCurrent {
		return fmt.Errorf("telemetry schema %d is newer than this binary (%d)", cur, telemetrySchemaCurrent)
	}
	for cur < telemetrySchemaCurrent {
		next := cur + 1
		var err error
		switch next {
		case 2:
			err = s.migrateV2ContentHash()
		default:
			err = fmt.Errorf("missing migration to schema %d", next)
		}
		if err != nil {
			return err
		}
		if err := s.setMeta("schema_version", strconv.Itoa(next)); err != nil {
			return err
		}
		cur = next
	}
	return nil
}

func (s *TelemetryStore) hasColumn(table, col string) (bool, error) {
	switch table {
	case "runs", "launch_events", "meta":
	default:
		return false, fmt.Errorf("unknown table %q", table)
	}
	rows, err := s.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == col {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (s *TelemetryStore) migrateV2ContentHash() error {
	if ok, err := s.hasColumn("runs", "content_sha256"); err != nil {
		return err
	} else if !ok {
		if _, err := s.db.Exec(`ALTER TABLE runs ADD COLUMN content_sha256 TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	if ok, err := s.hasColumn("launch_events", "content_sha256"); err != nil {
		return err
	} else if !ok {
		if _, err := s.db.Exec(`ALTER TABLE launch_events ADD COLUMN content_sha256 TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	if err := s.backfillHashes("runs", "extra_json"); err != nil {
		return err
	}
	if err := s.backfillHashes("launch_events", "payload_json"); err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM runs WHERE id NOT IN (SELECT MIN(id) FROM runs GROUP BY content_sha256)`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM launch_events WHERE id NOT IN (SELECT MIN(id) FROM launch_events GROUP BY content_sha256)`); err != nil {
		return err
	}
	_, err := s.db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS runs_content_sha256 ON runs(content_sha256);
		CREATE UNIQUE INDEX IF NOT EXISTS launch_content_sha256 ON launch_events(content_sha256);
	`)
	return err
}

func (s *TelemetryStore) backfillHashes(table, col string) error {
	rows, err := s.db.Query(`SELECT id, ` + col + ` FROM ` + table + ` WHERE content_sha256 = ''`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type row struct {
		id  int
		raw string
	}
	var pending []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.raw); err != nil {
			return err
		}
		pending = append(pending, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, r := range pending {
		if _, err := s.db.Exec(`UPDATE `+table+` SET content_sha256=? WHERE id=?`, contentSHA([]byte(r.raw)), r.id); err != nil {
			return err
		}
	}
	return nil
}
