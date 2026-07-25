package evocoupling_test

import (
	"testing"

	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/flamingoosesoftwareinc/uda/internal/evocoupling"
	"github.com/stretchr/testify/require"
)

func TestPackageResolver(t *testing.T) {
	t.Parallel()

	boundaries := []analyzer.PackageBoundary{
		{Name: "auth", Dirs: []string{"internal/auth"}},
		{Name: "auth_test", Dirs: []string{"internal/auth"}},
		{Name: "auth/oauth", Dirs: []string{"internal/auth/oauth"}},
		{Name: "billing", Dirs: []string{"internal/billing"}},
		{Name: "shared", Dirs: []string{"pkg/shared", "pkg/common"}},
	}

	r := evocoupling.NewPackageResolver(boundaries)

	cases := []struct {
		name string
		file string
		want []string
	}{
		{"exact_dir", "internal/auth/handler.go", []string{"auth", "auth_test"}},
		{"nested_prefers_deepest", "internal/auth/oauth/provider.go", []string{"auth/oauth"}},
		{"different_package", "internal/billing/invoice.go", []string{"billing"}},
		{"multi_dir_package", "pkg/shared/util.go", []string{"shared"}},
		{"multi_dir_package_alt", "pkg/common/types.go", []string{"shared"}},
		{"no_match", "vendor/foo/bar.go", nil},
		{"root_file", "main.go", nil},
		{"subdir_of_match", "internal/auth/middleware/cors.go", []string{"auth", "auth_test"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := r.Resolve(tc.file)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestResolveCommit(t *testing.T) {
	t.Parallel()

	boundaries := []analyzer.PackageBoundary{
		{Name: "auth", Dirs: []string{"internal/auth"}},
		{Name: "billing", Dirs: []string{"internal/billing"}},
	}

	r := evocoupling.NewPackageResolver(boundaries)

	files := []string{
		"internal/auth/handler.go",
		"internal/auth/model.go",
		"internal/billing/invoice.go",
		"README.md",
	}

	got := r.ResolveCommit(files)

	require.Equal(t, map[string]struct{}{"auth": {}, "billing": {}}, got)
}
