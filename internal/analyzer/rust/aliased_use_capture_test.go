package rust

import (
	"context"
	"testing"

	"github.com/flamingoosesoftwareinc/uda/internal/ts"
	"github.com/stretchr/testify/require"
	treesitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_rust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
)

// TestAliasedUseCaptureNoDuplicate directly tests whether tree-sitter
// produces duplicate captures for aliased use statements.
func TestAliasedUseCaptureNoDuplicate(t *testing.T) {
	code := []byte(`use std::collections::HashMap;
use tokio::runtime::Runtime as TokioRuntime;

fn main() {
    let _map: HashMap<String, String> = HashMap::new();
    let _rt = TokioRuntime::new().unwrap();
}
`)

	// Set up parser
	parser := treesitter.NewParser()
	defer parser.Close()

	lang := treesitter.NewLanguage(tree_sitter_rust.Language())
	err := parser.SetLanguage(lang)
	require.NoError(t, err)

	// Parse the code
	tree := parser.Parse(code, nil)
	defer tree.Close()

	// Run the query
	q, qc, err := ts.Query(context.Background(), parser, lang, tree, code, rustQuery)
	require.NoError(t, err)

	// Collect all captures
	caps := getCapturesFromMatches("test", "main.rs", q, qc, tree, code)

	t.Logf("Captured %d use statements:", len(caps.uses))

	for i, use := range caps.uses {
		t.Logf("  [%d] path=%q, alias=%q", i, use.path, use.alias)
	}

	// Count use statements
	hashMapCount := 0
	runtimeCount := 0

	for _, use := range caps.uses {
		if use.path == "std::collections::HashMap" {
			hashMapCount++
		}

		if use.path == "tokio::runtime::Runtime" {
			runtimeCount++
			// If aliased correctly, should have alias
			if use.alias != "" {
				require.Equal(t, "TokioRuntime", use.alias)
			}
		}
	}

	t.Logf("HashMap uses: %d, Runtime uses: %d", hashMapCount, runtimeCount)

	// Each should appear exactly once
	require.Equal(t, 1, hashMapCount, "HashMap should appear exactly once, got %d", hashMapCount)
	require.Equal(
		t,
		1,
		runtimeCount,
		"Runtime should appear exactly once, got %d (possible duplicate!)",
		runtimeCount,
	)
}
