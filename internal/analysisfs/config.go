package analysisfs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// configFileName is the repo-local config read for user ignore overrides.
const configFileName = ".uda.yaml"

// ErrNoConfig distinguishes "no analysis overrides declared" from a read or
// parse failure, so callers treat a missing section as defaults-only rather
// than an error.
var ErrNoConfig = errors.New("no analysis configuration")

// Config is the parsed analysis section of a repo's .uda.yaml. Exclude applies
// to every language; Languages holds per-language additions keyed by lowercase
// language name. Both are added on top of the built-in defaults.
type Config struct {
	Exclude   []string
	Languages map[string]LanguageRules
}

// LanguageRules holds the per-language ignore additions.
type LanguageRules struct {
	Exclude []string `yaml:"exclude"`
}

// LoadConfig reads the analysis section from the config file at path. A missing
// file or missing section returns ErrNoConfig.
func LoadConfig(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Config{}, fmt.Errorf("%w: %s does not exist", ErrNoConfig, path)
		}

		return Config{}, fmt.Errorf("reading %s: %w", path, err)
	}

	var file struct {
		Analysis map[string]yaml.Node `yaml:"analysis"`
	}

	if err := yaml.Unmarshal(raw, &file); err != nil {
		return Config{}, fmt.Errorf("parsing %s: %w", path, err)
	}

	if file.Analysis == nil {
		return Config{}, fmt.Errorf("%w in %s", ErrNoConfig, path)
	}

	return parseAnalysis(path, file.Analysis)
}

func parseAnalysis(path string, section map[string]yaml.Node) (Config, error) {
	cfg := Config{Languages: make(map[string]LanguageRules, len(section))}

	for key, node := range section {
		if key == "exclude" {
			if err := node.Decode(&cfg.Exclude); err != nil {
				return Config{}, fmt.Errorf("parsing %s analysis.exclude: %w", path, err)
			}

			continue
		}

		var rules LanguageRules
		if err := node.Decode(&rules); err != nil {
			return Config{}, fmt.Errorf("parsing %s analysis.%s: %w", path, key, err)
		}

		cfg.Languages[strings.ToLower(key)] = rules
	}

	return cfg, nil
}

// excludesFor returns the user-declared ignore patterns for a language: the
// global excludes plus that language's additions.
func (c Config) excludesFor(language string) []string {
	out := append([]string(nil), c.Exclude...)

	if rules, ok := c.Languages[strings.ToLower(language)]; ok {
		out = append(out, rules.Exclude...)
	}

	return out
}

// allExcludes returns every user-declared exclude — global plus every
// language's — for the detection path where the analysis language is not yet
// known. Sorted so the result is deterministic despite map iteration.
func (c Config) allExcludes() []string {
	out := append([]string(nil), c.Exclude...)

	for _, rules := range c.Languages {
		out = append(out, rules.Exclude...)
	}

	slices.Sort(out)

	return out
}
