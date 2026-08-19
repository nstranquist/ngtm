package gtm

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"go.yaml.in/yaml/v3"
)

const GEOSchemaVersion = 1

const (
	GEOKindPromptSet = "geo-prompt-set"
	GEOKindProbe     = "geo-probe"
	GEOKindMeasure   = "geo-measure"
)

// GEOProductConfig is the tracked, value-free contract for one product's GEO
// lifecycle. Credentials are resolved only from the process environment.
type GEOProductConfig struct {
	SchemaVersion int              `json:"schema_version" yaml:"schema_version"`
	Project       string           `json:"project" yaml:"project"`
	Product       string           `json:"product" yaml:"product"`
	Brand         string           `json:"brand" yaml:"brand"`
	Aliases       []string         `json:"aliases,omitempty" yaml:"aliases,omitempty"`
	Category      string           `json:"category,omitempty" yaml:"category,omitempty"`
	SiteURL       string           `json:"site_url,omitempty" yaml:"site_url,omitempty"`
	DemoURL       string           `json:"demo_url,omitempty" yaml:"demo_url,omitempty"`
	Install       string           `json:"install,omitempty" yaml:"install,omitempty"`
	Competitors   []GEOCompetitor  `json:"competitors,omitempty" yaml:"competitors,omitempty"`
	AIInfo        GEOAIInfo        `json:"ai_info,omitempty" yaml:"ai_info,omitempty"`
	Links         []GEOLink        `json:"links,omitempty" yaml:"links,omitempty"`
	Prompts       []GEOPrompt      `json:"prompts" yaml:"prompts"`
}

type GEOCompetitor struct {
	Name    string   `json:"name" yaml:"name"`
	Aliases []string `json:"aliases,omitempty" yaml:"aliases,omitempty"`
}

type GEOAIInfo struct {
	Type        string   `json:"type,omitempty" yaml:"type,omitempty"`
	Launch      string   `json:"launch,omitempty" yaml:"launch,omitempty"`
	Background  string   `json:"background,omitempty" yaml:"background,omitempty"`
	Features    []string `json:"features,omitempty" yaml:"features,omitempty"`
	IdealFor    []string `json:"ideal_for,omitempty" yaml:"ideal_for,omitempty"`
	Limitations []string `json:"limitations,omitempty" yaml:"limitations,omitempty"`
	Guidelines  []string `json:"guidelines,omitempty" yaml:"guidelines,omitempty"`
	Trust       []string `json:"trust,omitempty" yaml:"trust,omitempty"`
}

type GEOLink struct {
	Title string `json:"title" yaml:"title"`
	URL   string `json:"url" yaml:"url"`
	Note  string `json:"note,omitempty" yaml:"note,omitempty"`
}

type GEOPrompt struct {
	ID    string `json:"id" yaml:"id"`
	Text  string `json:"text" yaml:"text"`
	Topic string `json:"topic,omitempty" yaml:"topic,omitempty"`
	Kind  string `json:"kind,omitempty" yaml:"kind,omitempty"`
}

type GEOFinding struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

func LoadGEOProductConfig(path string) (GEOProductConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return GEOProductConfig{}, err
	}
	var cfg GEOProductConfig
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return GEOProductConfig{}, fmt.Errorf("parse GEO product config: %w", err)
	}
	if err := cfg.NormalizeAndValidate(); err != nil {
		return GEOProductConfig{}, err
	}
	return cfg, nil
}

func (c *GEOProductConfig) NormalizeAndValidate() error {
	if c.SchemaVersion == 0 {
		c.SchemaVersion = GEOSchemaVersion
	}
	if c.SchemaVersion != GEOSchemaVersion {
		return fmt.Errorf("GEO schema_version=%d, want %d", c.SchemaVersion, GEOSchemaVersion)
	}
	c.Project = normalizeSEOProject(c.Project)
	if c.Project == "" {
		return errors.New("GEO project is required")
	}
	c.Product = strings.TrimSpace(c.Product)
	if c.Product == "" {
		c.Product = c.Project
	}
	c.Brand = strings.TrimSpace(c.Brand)
	if c.Brand == "" {
		c.Brand = c.Product
	}
	c.Category = strings.TrimSpace(c.Category)
	c.SiteURL = strings.TrimRight(strings.TrimSpace(c.SiteURL), "/")
	c.DemoURL = strings.TrimRight(strings.TrimSpace(c.DemoURL), "/")
	c.Install = strings.TrimSpace(c.Install)
	c.Aliases = normalizeGEOAliases(append([]string{c.Brand, c.Product}, c.Aliases...))
	if err := normalizeGEOCompetitors(c.Competitors); err != nil {
		return err
	}
	if err := normalizeGEOPrompts(c.Prompts); err != nil {
		return err
	}
	if len(c.Prompts) == 0 {
		return errors.New("GEO config needs at least one prompt")
	}
	for i := range c.Links {
		c.Links[i].Title = strings.TrimSpace(c.Links[i].Title)
		c.Links[i].URL = strings.TrimSpace(c.Links[i].URL)
		c.Links[i].Note = strings.TrimSpace(c.Links[i].Note)
		if c.Links[i].Title == "" || c.Links[i].URL == "" {
			return errors.New("GEO link requires title and url")
		}
	}
	return nil
}

func normalizeGEOAliases(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		key := strings.ToLower(s)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, s)
	}
	return out
}

func normalizeGEOCompetitors(in []GEOCompetitor) error {
	seen := map[string]bool{}
	for i := range in {
		in[i].Name = strings.TrimSpace(in[i].Name)
		if in[i].Name == "" {
			return errors.New("GEO competitor name is required")
		}
		key := strings.ToLower(in[i].Name)
		if seen[key] {
			return fmt.Errorf("duplicate GEO competitor %q", in[i].Name)
		}
		seen[key] = true
		in[i].Aliases = normalizeGEOAliases(append([]string{in[i].Name}, in[i].Aliases...))
	}
	return nil
}

func normalizeGEOPrompts(in []GEOPrompt) error {
	seen := map[string]bool{}
	for i := range in {
		in[i].ID = seoSlugify(in[i].ID)
		in[i].Text = strings.Join(strings.Fields(strings.TrimSpace(in[i].Text)), " ")
		in[i].Topic = strings.TrimSpace(in[i].Topic)
		in[i].Kind = strings.ToLower(strings.TrimSpace(in[i].Kind))
		if in[i].ID == "" || in[i].Text == "" {
			return errors.New("GEO prompt requires id and text")
		}
		if seen[in[i].ID] {
			return fmt.Errorf("duplicate GEO prompt id %q", in[i].ID)
		}
		seen[in[i].ID] = true
		switch in[i].Kind {
		case "", "best", "alternative", "use-case":
		default:
			return fmt.Errorf("GEO prompt %s has unknown kind %q", in[i].ID, in[i].Kind)
		}
	}
	return nil
}

func (c GEOProductConfig) BrandNames() []geoBrand {
	out := []geoBrand{{Canonical: c.Brand, Aliases: c.Aliases, Ours: true}}
	for _, comp := range c.Competitors {
		out = append(out, geoBrand{Canonical: comp.Name, Aliases: comp.Aliases})
	}
	return out
}

type geoBrand struct {
	Canonical string
	Aliases   []string
	Ours      bool
}
