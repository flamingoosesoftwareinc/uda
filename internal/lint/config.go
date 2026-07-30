package lint

import (
	"errors"
	"fmt"
	"os"
	"sort"

	"gopkg.in/yaml.v3"
)

// lintKey is the top-level .uda.yaml section holding the policy.
// excludeKey (inside it) filters packages out of the graph; every other
// key inside the section is a language block.
const (
	lintKey    = "lint"
	excludeKey = "exclude"
	allowedKey = "allowed"
	pendingKey = "pending"
)

// ErrNoLintConfig distinguishes "no policy declared" from a read failure —
// the lint command turns it into a pointer at `uda lint init`.
var ErrNoLintConfig = errors.New("no lint configuration")

// Config is the parsed lint section of a repo's .uda.yaml.
type Config struct {
	Exclude   []string
	Languages map[string]Rules // keyed by lowercase language name
}

// LoadConfig reads the lint section from the config file at path.
// A missing file or missing lint section returns ErrNoLintConfig.
func LoadConfig(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, fmt.Errorf("%w: %s does not exist", ErrNoLintConfig, path)
		}

		return Config{}, fmt.Errorf("reading %s: %w", path, err)
	}

	var file struct {
		Lint map[string]yaml.Node `yaml:"lint"`
	}

	if err := yaml.Unmarshal(raw, &file); err != nil {
		return Config{}, fmt.Errorf("parsing %s: %w", path, err)
	}

	if file.Lint == nil {
		return Config{}, fmt.Errorf("%w in %s", ErrNoLintConfig, path)
	}

	cfg := Config{Languages: make(map[string]Rules, len(file.Lint))}

	for key, node := range file.Lint {
		if key == excludeKey {
			if err := node.Decode(&cfg.Exclude); err != nil {
				return Config{}, fmt.Errorf("parsing %s lint.exclude: %w", path, err)
			}

			continue
		}

		var rules Rules
		if err := node.Decode(&rules); err != nil {
			return Config{}, fmt.Errorf("parsing %s lint.%s: %w", path, key, err)
		}

		cfg.Languages[key] = rules
	}

	return cfg, nil
}

// WriteRules replaces the allowed and pending lists of one language block
// in the config file at path, creating the file, the lint section, or the
// block as needed. Everything else in the file — other sections, roles,
// forbid, comments — is preserved byte-for-byte by operating on the yaml
// node tree instead of re-marshalling a struct.
func WriteRules(path, language string, rules Rules) error {
	doc, err := loadDocument(path)
	if err != nil {
		return err
	}

	root := doc.Content[0]
	langNode := findOrCreateMap(findOrCreateMap(root, lintKey), language)

	setKey(langNode, allowedKey, allowedNode(rules.Allowed))
	setKey(langNode, pendingKey, pendingNode(rules.Pending))

	out, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}

	if err := os.WriteFile(
		path,
		out,
		0o644,
	); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	return nil
}

func loadDocument(path string) (*yaml.Node, error) {
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	// Empty or missing file: build a document around an empty mapping.
	if doc.Kind == 0 || len(doc.Content) == 0 {
		doc = yaml.Node{
			Kind:    yaml.DocumentNode,
			Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}},
		}
	}

	return &doc, nil
}

// findOrCreateMap returns the mapping node under key in parent, appending
// an empty one if absent.
func findOrCreateMap(parent *yaml.Node, key string) *yaml.Node {
	if existing := valueOf(parent, key); existing != nil {
		return existing
	}

	value := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	parent.Content = append(parent.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		value,
	)

	return value
}

// setKey replaces the value node under key in the mapping, appending the
// pair if absent.
func setKey(mapping *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content[i+1] = value

			return
		}
	}

	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		value,
	)
}

func valueOf(mapping *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}

	return nil
}

func allowedNode(allowed []string) *yaml.Node {
	sorted := make([]string, len(allowed))
	copy(sorted, allowed)
	sort.Strings(sorted)

	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, edge := range sorted {
		seq.Content = append(seq.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: edge})
	}

	return seq
}

func pendingNode(pending []PendingEdge) *yaml.Node {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}

	for _, p := range pending {
		entry := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		appendScalarPair(entry, "edge", p.Edge)

		if p.Added != "" {
			appendScalarPair(entry, "added", p.Added)
		}

		if p.By != "" {
			appendScalarPair(entry, "by", p.By)
		}

		seq.Content = append(seq.Content, entry)
	}

	return seq
}

func appendScalarPair(mapping *yaml.Node, key, value string) {
	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value},
	)
}
