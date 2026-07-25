package golang

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClassifyImport(t *testing.T) {
	t.Parallel()

	const udaModule = "github.com/flamingoosesoftwareinc/uda"

	tests := map[string]struct {
		importPath  string
		modulePaths []string
		wantKind    ImportKind
	}{
		"fmt is stdlib (exact match)": {
			importPath: "fmt",
			wantKind:   ImportStdlib,
		},
		"net/http is stdlib (exact match)": {
			importPath: "net/http",
			wantKind:   ImportStdlib,
		},
		"encoding/json is stdlib (exact match)": {
			importPath: "encoding/json",
			wantKind:   ImportStdlib,
		},
		"unlisted dotless path is stdlib (heuristic fallback)": {
			importPath: "futurepkg/subpkg",
			wantKind:   ImportStdlib,
		},
		"testify is external": {
			importPath:  "github.com/stretchr/testify/require",
			modulePaths: []string{udaModule},
			wantKind:    ImportExternal,
		},
		"golang.org/x is external": {
			importPath: "golang.org/x/tools/go/packages",
			wantKind:   ImportExternal,
		},
		"module subpackage is internal": {
			importPath:  udaModule + "/internal/analyzer",
			modulePaths: []string{udaModule},
			wantKind:    ImportInternal,
		},
		"module root is internal": {
			importPath:  udaModule,
			modulePaths: []string{udaModule},
			wantKind:    ImportInternal,
		},
		"internal beats stdlib heuristic for dotless module": {
			importPath:  "foo/bar",
			modulePaths: []string{"foo"},
			wantKind:    ImportInternal,
		},
		"sibling module prefix is not internal": {
			importPath:  udaModule + "-extra/pkg",
			modulePaths: []string{udaModule},
			wantKind:    ImportExternal,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := ClassifyImport(tt.importPath, tt.modulePaths)
			require.Equal(t, tt.wantKind, got)
		})
	}
}
