package gtm

import (
	"fmt"
	"strings"
)

// RunQuery is a bounded read over intel runs. Extra_json is never returned —
// that blob is how agents torch a context window.
type RunQuery struct {
	Vertical string
	Subject  string // substring match
	Limit    int
}

// RunRecord is the compact row agents should consume.
type RunRecord struct {
	TS          string   `json:"ts"`
	Vertical    string   `json:"vertical"`
	Subject     string   `json:"subject"`
	Surface     string   `json:"surface,omitempty"`
	Provider    string   `json:"provider,omitempty"`
	Verdict     string   `json:"verdict,omitempty"`
	PanelMedian *float64 `json:"panel_median,omitempty"`
}

const (
	defaultRunQueryLimit = 20
	maxRunQueryLimit     = 100
)

// QueryRuns returns the newest matching runs, newest first, capped.
func (s *TelemetryStore) QueryRuns(q RunQuery) ([]RunRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("telemetry store is closed")
	}
	limit := q.Limit
	if limit <= 0 {
		limit = defaultRunQueryLimit
	}
	if limit > maxRunQueryLimit {
		limit = maxRunQueryLimit
	}
	sql := `SELECT ts, vertical, subject, surface, provider, verdict, panel_median
		FROM runs WHERE 1=1`
	var args []any
	if v := strings.TrimSpace(q.Vertical); v != "" {
		sql += ` AND vertical = ?`
		args = append(args, v)
	}
	if sub := strings.TrimSpace(q.Subject); sub != "" {
		sql += ` AND subject LIKE ?`
		args = append(args, "%"+sub+"%")
	}
	sql += ` ORDER BY ts DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RunRecord
	for rows.Next() {
		var r RunRecord
		var median any
		if err := rows.Scan(&r.TS, &r.Vertical, &r.Subject, &r.Surface, &r.Provider, &r.Verdict, &median); err != nil {
			return nil, err
		}
		switch v := median.(type) {
		case float64:
			r.PanelMedian = &v
		case int64:
			f := float64(v)
			r.PanelMedian = &f
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// LaunchQuery is a bounded read over launch events. payload_json is never returned.
type LaunchQuery struct {
	Product string
	Type    string
	Limit   int
}

// LaunchRecord is the compact launch row agents should consume.
type LaunchRecord struct {
	TS      string `json:"ts"`
	Type    string `json:"type"`
	Product string `json:"product"`
	Week    string `json:"week,omitempty"`
	Channel string `json:"channel,omitempty"`
	Verdict string `json:"verdict,omitempty"`
}

// QueryLaunch returns the newest matching launch events, newest first, capped.
func (s *TelemetryStore) QueryLaunch(q LaunchQuery) ([]LaunchRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("telemetry store is closed")
	}
	limit := q.Limit
	if limit <= 0 {
		limit = defaultRunQueryLimit
	}
	if limit > maxRunQueryLimit {
		limit = maxRunQueryLimit
	}
	sql := `SELECT ts, type, product, payload_json FROM launch_events WHERE 1=1`
	var args []any
	if p := strings.TrimSpace(q.Product); p != "" {
		sql += ` AND product = ?`
		args = append(args, p)
	}
	if typ := strings.TrimSpace(q.Type); typ != "" {
		sql += ` AND type = ?`
		args = append(args, typ)
	}
	sql += ` ORDER BY ts DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LaunchRecord
	for rows.Next() {
		var r LaunchRecord
		var payload string
		if err := rows.Scan(&r.TS, &r.Type, &r.Product, &payload); err != nil {
			return nil, err
		}
		if ev, err := decodeLaunchEventStrict([]byte(payload)); err == nil {
			r.Week = ev.Week
			r.Channel = ev.Channel
			if ev.Verdict != "" {
				r.Verdict = string(ev.Verdict)
			}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
