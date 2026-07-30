package python

import (
	"context"
	"testing"

	"github.com/flamingoosesoftwareinc/uda/internal/ts"
	"github.com/stretchr/testify/require"
	treesitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
)

// captureUsageExprs parses code, runs the Python usage query, and returns the
// set of captured usage expressions.
func captureUsageExprs(t *testing.T, code string) map[string]bool {
	t.Helper()

	src := []byte(code)

	parser := treesitter.NewParser()
	defer parser.Close()

	lang := treesitter.NewLanguage(tree_sitter_python.Language())
	require.NoError(t, parser.SetLanguage(lang))

	tree := parser.Parse(src, nil)
	defer tree.Close()

	query, queryCursor, err := ts.Query(context.Background(), parser, lang, tree, src, pythonQuery)
	require.NoError(t, err)

	defer queryCursor.Close()

	caps := getCapturesFromMatches("test.module", "test.py", query, queryCursor, tree, src)

	usageExprs := make(map[string]bool)
	for _, u := range caps.usages {
		usageExprs[u.expr] = true
	}

	return usageExprs
}

func TestClassAndSubscriptCaptures(t *testing.T) {
	tests := map[string]struct {
		code     string
		expected []string
	}{
		"base classes": {
			code: `from mylib import Base, Mixin

class Foo(Base, Mixin):
    pass
`,
			expected: []string{"Base", "Mixin"},
		},
		"metaclass": {
			code: `from abc import ABCMeta

class Atom(metaclass=ABCMeta):
    pass
`,
			expected: []string{"ABCMeta"},
		},
		"subscript type": {
			code: `from typing import Optional, Dict

x: Optional[str] = None
y: Dict[str, int] = {}
`,
			expected: []string{"Optional", "Dict"},
		},
		"subscript argument": {
			code: `from typing import Optional, Dict
from mylib import Config

y: Dict[str, Config] = {}
z: Optional[Config] = None
`,
			expected: []string{"Config", "Optional", "Dict"},
		},
		"nested subscript": {
			code: `from typing import Dict, Optional

z: Dict[str, Optional[int]] = {}
`,
			expected: []string{"Dict", "Optional"},
		},
		"base classes and metaclass combined": {
			code: `from abc import ABCMeta
from mylib import BaseModel, Serializable

class MyModel(BaseModel, Serializable, metaclass=ABCMeta):
    pass
`,
			expected: []string{"BaseModel", "Serializable", "ABCMeta"},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			usageExprs := captureUsageExprs(t, tt.code)
			for _, want := range tt.expected {
				require.True(t, usageExprs[want], "should capture %q", want)
			}
		})
	}
}
