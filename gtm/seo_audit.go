package gtm

import (
	"bufio"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type SEOAuditReport struct {
	SchemaVersion int              `json:"schema_version"`
	Generated     string           `json:"generated"`
	Project       string           `json:"project"`
	Workspace     string           `json:"workspace"`
	Artifacts     []SEOArtifactRef `json:"artifacts"`
	Findings      []SEOFinding     `json:"findings"`
	Blockers      int              `json:"blockers"`
	Warnings      int              `json:"warnings"`
	Passed        bool             `json:"passed"`
}

func AuditSEOStore(store *SEOStore, cfg SEOProjectConfig) *SEOAuditReport {
	report := &SEOAuditReport{SchemaVersion: SEOSchemaVersion, Generated: nowSEO(nil), Project: cfg.Project, Workspace: store.Root}
	var research SEOResearchReport
	if ref, err := store.LoadLatest("research", &research); err != nil {
		report.Findings = append(report.Findings, SEOFinding{Code: "AUDIT_RESEARCH_MISSING", Severity: "blocker", Message: err.Error()})
	} else {
		report.Artifacts = append(report.Artifacts, ref)
		auditSEOSchema(report, "research", research.SchemaVersion)
		if digest, digestErr := digestSEOValue(cfg); digestErr != nil {
			report.Findings = append(report.Findings, SEOFinding{Code: "AUDIT_CONFIG_DIGEST_FAILED", Severity: "blocker", Message: digestErr.Error()})
		} else if research.ConfigDigest != digest {
			report.Findings = append(report.Findings, SEOFinding{Code: "AUDIT_CONFIG_DRIFT", Severity: "blocker", Message: "latest research was produced from a different project configuration"})
		}
		if !research.Passed {
			report.Findings = append(report.Findings, SEOFinding{Code: "AUDIT_RESEARCH_BLOCKED", Severity: "blocker", Message: "latest research does not meet its configured evidence requirements"})
		}
	}
	var brief SEOBrief
	if ref, err := store.LoadLatest("brief", &brief); err == nil {
		report.Artifacts = append(report.Artifacts, ref)
		auditSEOSchema(report, "brief", brief.SchemaVersion)
		if brief.ResearchID != research.ID {
			report.Findings = append(report.Findings, SEOFinding{Code: "AUDIT_BRIEF_RESEARCH_MISMATCH", Severity: "blocker", Message: "latest brief does not reference latest research"})
		}
		if !brief.Passed {
			report.Findings = append(report.Findings, SEOFinding{Code: "AUDIT_BRIEF_BLOCKED", Severity: "blocker", Message: "latest brief is blocked"})
		}
	} else if !os.IsNotExist(err) {
		report.Findings = append(report.Findings, SEOFinding{Code: "AUDIT_BRIEF_INVALID", Severity: "blocker", Message: err.Error()})
	} else {
		report.Findings = append(report.Findings, SEOFinding{Code: "AUDIT_BRIEF_ABSENT", Severity: "warning", Message: "no brief artifact yet"})
	}
	var publish SEOPublishManifest
	var publishRef SEOArtifactRef
	if ref, err := store.LoadLatest("publish", &publish); err == nil {
		publishRef = ref
		report.Artifacts = append(report.Artifacts, ref)
		auditSEOSchema(report, "publish", publish.SchemaVersion)
		if !publish.Passed {
			report.Findings = append(report.Findings, SEOFinding{Code: "AUDIT_PUBLISH_BLOCKED", Severity: "blocker", Message: "latest publish manifest has blockers"})
		}
		auditSEOPublish(report, cfg, publish, brief.ID)
	} else if !os.IsNotExist(err) {
		report.Findings = append(report.Findings, SEOFinding{Code: "AUDIT_PUBLISH_INVALID", Severity: "blocker", Message: err.Error()})
	} else {
		report.Findings = append(report.Findings, SEOFinding{Code: "AUDIT_PUBLISH_ABSENT", Severity: "warning", Message: "no publish artifact yet"})
	}
	var measurement SEOMeasurementReport
	if ref, err := store.LoadLatest("measurement", &measurement); err == nil {
		report.Artifacts = append(report.Artifacts, ref)
		auditSEOSchema(report, "measurement", measurement.SchemaVersion)
		if measurement.ResearchID != research.ID {
			report.Findings = append(report.Findings, SEOFinding{Code: "AUDIT_MEASUREMENT_RESEARCH_MISMATCH", Severity: "blocker", Message: "latest measurement does not reference latest research"})
		}
		if publishRef.ID != "" && measurement.PublishID != publishRef.ID {
			report.Findings = append(report.Findings, SEOFinding{Code: "AUDIT_MEASUREMENT_PUBLISH_MISMATCH", Severity: "blocker", Message: "latest measurement does not reference latest publish artifact"})
		}
		if !measurement.Passed {
			report.Findings = append(report.Findings, SEOFinding{Code: "AUDIT_MEASUREMENT_BLOCKED", Severity: "blocker", Message: "latest measurement has blockers"})
		}
	} else if os.IsNotExist(err) {
		report.Findings = append(report.Findings, SEOFinding{Code: "AUDIT_MEASUREMENT_ABSENT", Severity: "warning", Message: "no measurement artifact yet"})
	} else {
		report.Findings = append(report.Findings, SEOFinding{Code: "AUDIT_MEASUREMENT_INVALID", Severity: "blocker", Message: err.Error()})
	}
	var retro SEORetroReport
	if ref, err := store.LoadLatest("retro", &retro); err == nil {
		report.Artifacts = append(report.Artifacts, ref)
		auditSEOSchema(report, "retro", retro.SchemaVersion)
		if retro.ResearchID != research.ID || retro.MeasurementID != measurement.ID {
			report.Findings = append(report.Findings, SEOFinding{Code: "AUDIT_RETRO_INPUT_MISMATCH", Severity: "blocker", Message: "latest retro does not reference latest research and measurement"})
		}
		if !retro.Passed {
			report.Findings = append(report.Findings, SEOFinding{Code: "AUDIT_RETRO_BLOCKED", Severity: "blocker", Message: "latest retro has blockers"})
		}
	} else if os.IsNotExist(err) {
		report.Findings = append(report.Findings, SEOFinding{Code: "AUDIT_RETRO_ABSENT", Severity: "warning", Message: "no retro artifact yet"})
	} else {
		report.Findings = append(report.Findings, SEOFinding{Code: "AUDIT_RETRO_INVALID", Severity: "blocker", Message: err.Error()})
	}
	auditSEOEvents(report, filepath.Join(store.Root, "events.jsonl"))
	for _, f := range report.Findings {
		switch f.Severity {
		case "blocker":
			report.Blockers++
		case "warning":
			report.Warnings++
		}
	}
	report.Passed = report.Blockers == 0
	return report
}

func auditSEOSchema(report *SEOAuditReport, kind string, version int) {
	if version != SEOSchemaVersion {
		report.Findings = append(report.Findings, SEOFinding{
			Code: "AUDIT_SCHEMA_INVALID", Severity: "blocker",
			Message: fmt.Sprintf("%s schema_version=%d, want %d", kind, version, SEOSchemaVersion),
		})
	}
}

func auditSEOPublish(report *SEOAuditReport, cfg SEOProjectConfig, publish SEOPublishManifest, briefID string) {
	outputDir, outputErr := filepath.Abs(publish.OutputDir)
	if outputErr != nil || strings.TrimSpace(publish.OutputDir) == "" {
		report.Findings = append(report.Findings, SEOFinding{Code: "AUDIT_PUBLISH_OUTPUT_INVALID", Severity: "blocker", Message: "publish output directory is missing or invalid"})
	}
	seenSlugs := map[string]bool{}
	seenCanonicals := map[string]bool{}
	for _, page := range publish.Pages {
		if page.Slug == "" || seenSlugs[page.Slug] {
			report.Findings = append(report.Findings, SEOFinding{Code: "AUDIT_PUBLISH_SLUG_DUPLICATE", Severity: "blocker", Path: page.Path, Message: "published page slug is empty or duplicated"})
		}
		seenSlugs[page.Slug] = true
		if page.Canonical != "" {
			canonicalKey := strings.ToLower(page.Canonical)
			if seenCanonicals[canonicalKey] {
				report.Findings = append(report.Findings, SEOFinding{Code: "AUDIT_PUBLISH_CANONICAL_DUPLICATE", Severity: "blocker", Path: page.Path, Message: "published canonical is duplicated"})
			}
			seenCanonicals[canonicalKey] = true
		}
		if page.BriefID != briefID {
			report.Findings = append(report.Findings, SEOFinding{Code: "AUDIT_PUBLISH_BRIEF_MISMATCH", Severity: "blocker", Path: page.Path, Message: "published page does not reference latest brief"})
		}
		b, err := os.ReadFile(page.Path)
		if err != nil {
			report.Findings = append(report.Findings, SEOFinding{Code: "AUDIT_PAGE_MISSING", Severity: "blocker", Path: page.Path, Message: err.Error()})
			continue
		}
		html := string(b)
		if outputErr == nil {
			pagePath, pathErr := filepath.Abs(page.Path)
			if pathErr != nil || filepath.Dir(pagePath) != outputDir || filepath.Base(pagePath) != page.Slug+".html" {
				report.Findings = append(report.Findings, SEOFinding{Code: "AUDIT_PAGE_PATH_INVALID", Severity: "blocker", Path: page.Path, Message: "published page path is not the expected slug file inside output_dir"})
			}
		}
		if !strings.Contains(html, `name="robots" content="`+page.Robots+`"`) {
			report.Findings = append(report.Findings, SEOFinding{Code: "AUDIT_ROBOTS_MISMATCH", Severity: "blocker", Path: page.Path, Message: "rendered robots metadata differs from manifest"})
		}
		if page.Indexable {
			if !publish.Approved || !publish.IndexRequested || hasSEOBlockers(page.Findings) {
				report.Findings = append(report.Findings, SEOFinding{Code: "AUDIT_INDEXABILITY_INVALID", Severity: "blocker", Path: page.Path, Message: "indexable page lacks approved, requested, blocker-free manifest state"})
			}
			if !validSEOCanonical(page.Canonical, cfg.Domain) || !strings.Contains(html, `rel="canonical" href="`+page.Canonical+`"`) {
				report.Findings = append(report.Findings, SEOFinding{Code: "AUDIT_CANONICAL_MISMATCH", Severity: "blocker", Path: page.Path, Message: "indexable page canonical is missing or invalid"})
			}
			if !strings.Contains(html, `application/ld+json`) {
				report.Findings = append(report.Findings, SEOFinding{Code: "AUDIT_STRUCTURED_DATA_MISSING", Severity: "blocker", Path: page.Path, Message: "indexable page lacks structured data"})
			}
		}
	}
	if publish.IndexRequested && cfg.Publishing.RequireSitemap {
		if publish.SitemapPath == "" {
			report.Findings = append(report.Findings, SEOFinding{Code: "AUDIT_SITEMAP_MISSING", Severity: "blocker", Message: "indexable publish has no sitemap path"})
		} else if sitemap, err := os.ReadFile(publish.SitemapPath); err != nil {
			report.Findings = append(report.Findings, SEOFinding{Code: "AUDIT_SITEMAP_MISSING", Severity: "blocker", Path: publish.SitemapPath, Message: err.Error()})
		} else {
			body := string(sitemap)
			for _, page := range publish.Pages {
				if page.Indexable && !strings.Contains(body, page.Canonical) && !strings.Contains(body, html.EscapeString(page.Canonical)) {
					report.Findings = append(report.Findings, SEOFinding{Code: "AUDIT_SITEMAP_ENTRY_MISSING", Severity: "blocker", Path: publish.SitemapPath, Message: "sitemap does not include indexable canonical " + page.Canonical})
				}
			}
		}
	}
}

func auditSEOEvents(report *SEOAuditReport, path string) {
	f, err := os.Open(path)
	if err != nil {
		report.Findings = append(report.Findings, SEOFinding{Code: "AUDIT_EVENT_LOG_MISSING", Severity: "blocker", Path: path, Message: err.Error()})
		return
	}
	defer f.Close()
	previous := ""
	s := bufio.NewScanner(f)
	line := 0
	for s.Scan() {
		line++
		var event struct {
			TS string `json:"ts"`
		}
		if err := json.Unmarshal(s.Bytes(), &event); err != nil {
			report.Findings = append(report.Findings, SEOFinding{Code: "AUDIT_EVENT_INVALID", Severity: "blocker", Path: path, Message: fmt.Sprintf("line %d: %v", line, err)})
			continue
		}
		if _, err := time.Parse(time.RFC3339, event.TS); err != nil {
			report.Findings = append(report.Findings, SEOFinding{Code: "AUDIT_EVENT_TIMESTAMP_INVALID", Severity: "blocker", Path: path, Message: fmt.Sprintf("line %d has an invalid RFC3339 timestamp", line)})
			continue
		}
		if event.TS < previous {
			report.Findings = append(report.Findings, SEOFinding{Code: "AUDIT_EVENT_ORDER", Severity: "blocker", Path: path, Message: fmt.Sprintf("line %d is out of timestamp order", line)})
		}
		previous = event.TS
	}
	if err := s.Err(); err != nil {
		report.Findings = append(report.Findings, SEOFinding{Code: "AUDIT_EVENT_READ", Severity: "blocker", Path: path, Message: err.Error()})
	}
}

func (r *SEOAuditReport) Markdown() string {
	var out strings.Builder
	fmt.Fprintf(&out, "# SEO audit: %s\n\n- Status: %s\n- Artifacts: %d\n- Blockers: %d\n- Warnings: %d\n\n", r.Project, passLabel(r.Passed), len(r.Artifacts), r.Blockers, r.Warnings)
	for _, f := range r.Findings {
		fmt.Fprintf(&out, "- **%s** `%s`: %s\n", strings.ToUpper(f.Severity), f.Code, f.Message)
	}
	return out.String()
}
