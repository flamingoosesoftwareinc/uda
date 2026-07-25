package ui

import (
	"encoding/json"
	"strings"
	"text/template"
)

// LintViolation is one gate failure, shaped for output.
type LintViolation struct {
	Kind string `json:"kind"`
	Edge string `json:"edge"`
	Rule string `json:"rule,omitempty"`
}

// LintLanguageReport groups violations for one language block.
type LintLanguageReport struct {
	Language   string          `json:"language"`
	Violations []LintViolation `json:"violations"`
}

// LintReport is the full lint verdict across language blocks.
type LintReport struct {
	Languages []LintLanguageReport `json:"languages"`
}

// Total returns the number of violations across all languages.
func (r LintReport) Total() int {
	total := 0
	for _, lang := range r.Languages {
		total += len(lang.Violations)
	}

	return total
}

// lintTextTemplate renders one line per violation:
//
//	go: unlisted  internal/domain -> internal/cache
//	go: forbidden internal/domain -> internal/adapter/http (rule: internal/domain -> internal/adapter/**)
var lintTextTemplate = template.Must(template.New("lint").Parse(
	`{{- range $lang := .Languages -}}
{{- range $lang.Violations -}}
{{ $lang.Language }}: {{ printf "%-9s" .Kind }} {{ .Edge }}{{ with .Rule }} (rule: {{ . }}){{ end }}
{{ end -}}
{{- end -}}`))

// LintJSON renders the report as indented JSON.
func LintJSON(report LintReport) (string, error) {
	b, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", err
	}

	return string(b), nil
}

// LintText renders the report through lintTextTemplate.
func LintText(report LintReport) string {
	var b strings.Builder

	// The template only touches report fields; execution cannot fail.
	_ = lintTextTemplate.Execute(&b, report)

	return b.String()
}
