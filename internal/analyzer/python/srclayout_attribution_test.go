package python_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/flamingoosesoftwareinc/uda/internal/analyzer/python"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSrcLayoutRootFilesNotAttributedToPackage(t *testing.T) {
	// Fixture: src-layout project with setup.py at root and tests/ outside src/.
	//
	// Structure:
	//   pyproject.toml
	//   setup.py                           <-- root build script, not part of the package
	//   src/hypothesis_jsonschema/__init__.py
	//   src/hypothesis_jsonschema/core.py
	//   tests/test_core.py                 <-- no __init__.py, outside src tree
	tests := map[string]struct {
		strategy python.BoundaryStrategy
	}{
		"module":     {strategy: python.StrategyModule},
		"package":    {strategy: python.StrategyPackage},
		"subpackage": {strategy: python.StrategySubpackage},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := os.DirFS(".testdata/srclayout_with_root_files")
			a := python.PythonAnalyzer(python.WithBoundaryStrategy(tt.strategy))
			got, err := a.Analyze(context.Background(), dir)
			require.NoError(t, err)

			packages := make([]string, 0, len(got))
			for _, m := range got {
				packages = append(packages, string(m.Package))
			}

			// Bug 1: setup.py at repo root must NOT appear under the package namespace.
			// Current broken behavior produces "hypothesis_jsonschema.setup".
			for _, pkg := range packages {
				assert.False(
					t,
					pkg == "hypothesis_jsonschema.setup" || strings.HasSuffix(pkg, ".setup"),
					"setup.py at repo root was attributed to package namespace as %q; all packages: %v",
					pkg,
					packages,
				)
			}

			// Bug 2: tests/ directory without __init__.py, outside src tree,
			// must NOT be conflated under the library's namespace.
			// Current broken behavior produces "hypothesis_jsonschema.tests"
			// or "hypothesis_jsonschema.tests.test_core".
			for _, pkg := range packages {
				assert.NotContains(
					t,
					pkg,
					"hypothesis_jsonschema.tests",
					"tests/ directory outside src tree was attributed under package namespace as %q; all packages: %v",
					pkg,
					packages,
				)
			}
		})
	}
}
