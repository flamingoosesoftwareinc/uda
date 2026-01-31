package golang

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetPkgPathPrefix(t *testing.T) {
	tests := map[string]struct {
		goFilepath string
		gomodPaths map[directory]modulePath
		want       modulePath
	}{
		"should return module path with directory when go.mod found in parent": {
			goFilepath: "src/cmd/main.go",
			gomodPaths: map[directory]modulePath{
				"src": "github.com/example/project",
			},
			want: "github.com/example/project/cmd",
		},
		"should return module path with directory when go.mod found in grandparent": {
			goFilepath: "src/pkg/handler/handler.go",
			gomodPaths: map[directory]modulePath{
				"src": "github.com/example/project",
			},
			want: "github.com/example/project/pkg/handler",
		},
		"should return module path for root go.mod": {
			goFilepath: "cmd/main.go",
			gomodPaths: map[directory]modulePath{
				".": "github.com/example/project",
			},
			want: "github.com/example/project/cmd",
		},
		"should return module path when go.mod is in nested src/go/src": {
			goFilepath: "src/go/src/cmd/main.go",
			gomodPaths: map[directory]modulePath{
				"src/go/src": "github.com/example/nested",
			},
			want: "github.com/example/nested/cmd",
		},
		"should return module path when file is deeply nested under src/go/src": {
			goFilepath: "src/go/src/pkg/handler/handler.go",
			gomodPaths: map[directory]modulePath{
				"src/go/src": "github.com/example/nested",
			},
			want: "github.com/example/nested/pkg/handler",
		},
		"should return file dir when file is directly in src/go/src": {
			goFilepath: "src/go/src/main.go",
			gomodPaths: map[directory]modulePath{
				"src/go/src": "github.com/example/nested",
			},
			want: "github.com/example/nested",
		},
		"should return file dir when no go.mod found": {
			goFilepath: "src/cmd/main.go",
			gomodPaths: map[directory]modulePath{},
			want:       "src/cmd",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := getPkgPathPrefix(tt.goFilepath, tt.gomodPaths)
			require.Equal(t, tt.want, got)
		})
	}
}
