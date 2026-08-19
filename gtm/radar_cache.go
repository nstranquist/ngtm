package gtm

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	radarCacheSchema = "nicos.gtm.radar.v1"
	radarGlanceCap   = 3
	radarTZName      = "America/Chicago"
)

// RadarItem is one compact launch row in the same-day cache.
type RadarItem struct {
	Feed       string `json:"feed"`
	Title      string `json:"title"`
	URL        string `json:"url,omitempty"`
	Snippet    string `json:"snippet,omitempty"`
	GitHubRepo string `json:"github_repo,omitempty"`
}

// RadarCache is the same-day glance file written by feeds browse --cache.
type RadarCache struct {
	Schema      string      `json:"schema"`
	Day         string      `json:"day"`
	GeneratedAt string      `json:"generated_at"`
	Items       []RadarItem `json:"items"`
	Missing     bool        `json:"missing,omitempty"`
}

func radarLocation() *time.Location {
	loc, err := time.LoadLocation(radarTZName)
	if err != nil {
		return time.FixedZone("CST", -6*60*60)
	}
	return loc
}

// RadarDayKey is YYYY-MM-DD in America/Chicago.
func RadarDayKey(now time.Time) string {
	return now.In(radarLocation()).Format("2006-01-02")
}

// DefaultRadarCachePath is ~/.nicos-dev/gtm/radar-YYYYMMDD.json unless
// NGTM_RADAR_CACHE is set.
func DefaultRadarCachePath(now time.Time) (string, error) {
	if p := strings.TrimSpace(os.Getenv("NGTM_RADAR_CACHE")); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	day := now.In(radarLocation()).Format("20060102")
	return filepath.Join(home, ".nicos-dev", "gtm", "radar-"+day+".json"), nil
}

// EvidenceToRadarItems copies launch evidence into the compact cache rows.
func EvidenceToRadarItems(ev []Evidence) []RadarItem {
	out := make([]RadarItem, 0, len(ev))
	for _, e := range ev {
		item := RadarItem{Feed: e.Feed, Title: e.Title, URL: e.URL, Snippet: e.Snippet}
		if e.Extra != nil {
			item.GitHubRepo = e.Extra["github_repo"]
		}
		out = append(out, item)
	}
	return out
}

// WriteRadarCache writes schema nicos.gtm.radar.v1 to path.
func WriteRadarCache(path string, items []RadarItem, now time.Time) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("radar cache path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	cache := RadarCache{
		Schema:      radarCacheSchema,
		Day:         RadarDayKey(now),
		GeneratedAt: now.UTC().Format(time.RFC3339),
		Items:       items,
	}
	raw, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

// ReadRadarCache returns today's cache. Missing or wrong-day files fail open.
func ReadRadarCache(path string, now time.Time) (RadarCache, error) {
	empty := RadarCache{Schema: radarCacheSchema, Day: RadarDayKey(now), Missing: true}
	if strings.TrimSpace(path) == "" {
		return empty, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return empty, nil
		}
		return empty, err
	}
	var cache RadarCache
	if err := json.Unmarshal(raw, &cache); err != nil {
		return empty, fmt.Errorf("decode radar cache: %w", err)
	}
	if cache.Schema != radarCacheSchema || cache.Day != RadarDayKey(now) {
		return empty, nil
	}
	cache.Missing = false
	return cache, nil
}

// GlanceRadarItems returns at most 3 titles from a cache.
func GlanceRadarItems(cache RadarCache, limit int) []RadarItem {
	if limit <= 0 {
		limit = radarGlanceCap
	}
	if len(cache.Items) < limit {
		limit = len(cache.Items)
	}
	out := make([]RadarItem, 0, limit)
	pick := func(requireGitHub bool) {
		for _, item := range cache.Items {
			if len(out) == limit {
				return
			}
			if strings.TrimSpace(item.Title) == "" {
				continue
			}
			hasRepo := strings.TrimSpace(item.GitHubRepo) != ""
			if requireGitHub != hasRepo {
				continue
			}
			out = append(out, item)
		}
	}
	pick(true)
	pick(false)
	return out
}
