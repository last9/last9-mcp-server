package dashboards

import (
	"embed"
	"fmt"

	"gopkg.in/yaml.v3"
)

//go:embed all:templates catalog.yaml
var templateFS embed.FS

// TemplateInfo describes a dashboard template available for use with create_dashboard_from_template.
type TemplateInfo struct {
	ID            string   `json:"id" yaml:"id"`
	DisplayName   string   `json:"display_name" yaml:"display_name"`
	Platform      string   `json:"platform,omitempty" yaml:"platform"`
	RequiredKnobs []string `json:"required_knobs" yaml:"required_knobs"`
}

type catalogFile struct {
	Templates []TemplateInfo `yaml:"templates"`
}

func templatePath(id string) string {
	return fmt.Sprintf("templates/%s/dashboard.api.json.tmpl", id)
}

// ListTemplates returns all templates registered in catalog.yaml that have a dashboard.api.json.tmpl.
func ListTemplates() ([]TemplateInfo, error) {
	raw, err := templateFS.ReadFile("catalog.yaml")
	if err != nil {
		return nil, fmt.Errorf("read catalog: %w", err)
	}

	var cat catalogFile
	if err := yaml.Unmarshal(raw, &cat); err != nil {
		return nil, fmt.Errorf("parse catalog: %w", err)
	}

	var result []TemplateInfo
	for _, t := range cat.Templates {
		if _, err := templateFS.ReadFile(templatePath(t.ID)); err == nil {
			result = append(result, t)
		}
	}
	return result, nil
}

// loadTemplate returns the raw content of a template by id.
func loadTemplate(id string) (string, error) {
	raw, err := templateFS.ReadFile(templatePath(id))
	if err != nil {
		return "", fmt.Errorf("template %q not found", id)
	}
	return string(raw), nil
}
