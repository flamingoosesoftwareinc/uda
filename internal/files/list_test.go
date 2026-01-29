package files

import (
	"context"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

func TestListFiles(t *testing.T) {
	tests := map[string]struct {
		args    fstest.MapFS
		options []FileFilter
		want    []string
		err     error
	}{
		"should return values": {
			args: fstest.MapFS{
				"go": {
					Mode: fs.ModeDir,
				},
				"go/main.go": {},
				"web": {
					Mode: fs.ModeDir,
				},
				"web/index.ts": {},
			},
			want: []string{
				"go/main.go",
				"web/index.ts",
			},
		},
		"should skip hidden directories": {
			args: fstest.MapFS{
				".git": {
					Mode: fs.ModeDir,
				},
				".git/hooks": {
					Mode: fs.ModeDir,
				},
				".git/hooks/pre-commit": {},
				"go": {
					Mode: fs.ModeDir,
				},
				"go/main.go": {},
				"web": {
					Mode: fs.ModeDir,
				},
				"web/index.ts": {},
			},
			options: []FileFilter{SkipHidden()},
			want: []string{
				"go/main.go",
				"web/index.ts",
			},
		},
		"should skip hidden files and directories": {
			args: fstest.MapFS{
				".git": {
					Mode: fs.ModeDir,
				},
				".git/hooks": {
					Mode: fs.ModeDir,
				},
				".git/hooks/pre-commit": {},
				".gitignore":            {},
				"go": {
					Mode: fs.ModeDir,
				},
				"go/main.go": {},
				"web": {
					Mode: fs.ModeDir,
				},
				"web/index.ts": {},
			},
			options: []FileFilter{SkipHidden()},
			want: []string{
				"go/main.go",
				"web/index.ts",
			},
		},
	}

	ctx := context.Background()
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := ListFiles(ctx, tt.args, tt.options...)
			require.NoError(t, err)

			require.ElementsMatch(t, got, tt.want)
		})
	}
}
