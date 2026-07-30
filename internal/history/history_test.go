package history_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/flamingoosesoftwareinc/uda/internal/cache"
	udagit "github.com/flamingoosesoftwareinc/uda/internal/git"
	"github.com/flamingoosesoftwareinc/uda/internal/history"
	"github.com/sebdah/goldie/v2"
	"github.com/stretchr/testify/require"
)

// gitDir is the on-disk git metadata directory; named so the fixture-repo
// helpers don't repeat the literal at every site.
const gitDir = ".git"

// copyTestRepo copies a testdata directory to a temp location and renames dot-git to .git.
func copyTestRepo(t *testing.T, testdataDir string) string {
	t.Helper()

	tempDir := t.TempDir()

	err := filepath.WalkDir(testdataDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(testdataDir, path)
		if err != nil {
			return err
		}

		// Rename dot-git to .git
		if relPath == "dot-git" || filepath.Base(relPath) == "dot-git" {
			relPath = filepath.Join(filepath.Dir(relPath), gitDir)
			if relPath == gitDir {
				relPath = gitDir
			}
		} else if len(relPath) > 8 && relPath[:8] == "dot-git/" {
			relPath = gitDir + relPath[7:]
		}

		destPath := filepath.Join(tempDir, relPath)

		if d.IsDir() {
			return os.MkdirAll(destPath, 0o755)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		return os.WriteFile(destPath, data, 0o644)
	})
	require.NoError(t, err)

	return tempDir
}

// setupTestRepo creates a test repo with cache, returning the common dependencies.
func setupTestRepo(t *testing.T, testdataDir string) (*udagit.Repository, *cache.Cache) {
	t.Helper()
	repoPath := copyTestRepo(t, testdataDir)
	repo, err := udagit.OpenRepository(repoPath)
	require.NoError(t, err)
	c := cache.NewCache(cache.Config{BaseDir: t.TempDir()})

	return repo, c
}

func TestHistoryAnalyzer_AnalyzeRange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		testdata  string
		commitish string
		wantCount int
	}{
		{"SingleCommit", ".testdata/single_commit", "HEAD", 1},
		{"CommitRange", ".testdata/commit_range", "HEAD~3..HEAD", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo, c := setupTestRepo(t, tt.testdata)
			a := history.NewAnalyzer(repo, c, "go")

			commitRange, err := udagit.ParseCommitRange(repo, tt.commitish)
			require.NoError(t, err)

			results, err := a.AnalyzeRange(context.Background(), commitRange)
			require.NoError(t, err)
			require.Len(t, results, tt.wantCount)

			normalized := make([]map[string]any, len(results))
			for i, r := range results {
				normalized[i] = normalizeCommitMetrics(r)
			}

			g := goldie.New(t,
				goldie.WithFixtureDir(tt.testdata),
				goldie.WithNameSuffix(".json"),
			)

			g.AssertJson(t, "golden", normalized)
		})
	}
}

func TestHistoryAnalyzer_CacheHit(t *testing.T) {
	t.Parallel()

	repo, c := setupTestRepo(t, ".testdata/single_commit")
	a := history.NewAnalyzer(repo, c, "go")

	commitRange, err := udagit.ParseCommitRange(repo, "HEAD")
	require.NoError(t, err)

	// First analysis - populates cache
	results1, err := a.AnalyzeRange(context.Background(), commitRange)
	require.NoError(t, err)

	// Get the HEAD SHA
	headSHA, err := repo.ResolveCommit("HEAD")
	require.NoError(t, err)

	// Verify cache file exists
	require.True(t, c.Has(repo.RepoID(), headSHA.String()))

	// Second analysis - should hit cache
	results2, err := a.AnalyzeRange(context.Background(), commitRange)
	require.NoError(t, err)

	// Results should be identical
	require.Len(t, results2, len(results1))
	require.Equal(t, results1[0].Metrics, results2[0].Metrics)
}

func TestWorkspace_Checkout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		commitish string
		golden    string
	}{
		{"HEAD~2", "HEAD~2", "checkout_head_minus_2"},
		{"HEAD", "HEAD", "checkout_head"},
	}

	repo, c := setupTestRepo(t, ".testdata/commit_range")
	workspacePath := c.WorkspacePath(repo.RepoID())
	ws, err := history.NewWorkspace(repo, workspacePath)
	require.NoError(t, err)

	g := goldie.New(t,
		goldie.WithFixtureDir(".testdata/commit_range"),
		goldie.WithNameSuffix(".json"),
	)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash, err := repo.ResolveCommit(tt.commitish)
			require.NoError(t, err)
			require.NoError(t, ws.Checkout(hash))

			snapshot := snapshotWorkspace(t, ws.Path())
			g.AssertJson(t, tt.golden, snapshot)
		})
	}
}

// snapshotWorkspace walks the workspace directory and returns a sorted slice of
// file entries (path + content) for golden comparison. The .git directory is excluded.
func snapshotWorkspace(t *testing.T, root string) []fileEntry {
	t.Helper()

	var entries []fileEntry

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		// Skip .git directory
		if rel == ".git" || strings.HasPrefix(rel, ".git/") {
			if d.IsDir() {
				return filepath.SkipDir
			}

			return nil
		}

		if d.IsDir() {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		entries = append(entries, fileEntry{
			Path:    rel,
			Content: string(content),
		})

		return nil
	})
	require.NoError(t, err)

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Path < entries[j].Path
	})

	return entries
}

type fileEntry struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// normalizeCommitMetrics removes variable fields (SHA, timestamp) and sorts metrics for golden comparison.
func normalizeCommitMetrics(cm history.CommitMetrics) map[string]any {
	// Sort metrics within each language for deterministic comparison
	sortedMetrics := make([]analyzer.LanguageMetricsSummary, len(cm.Metrics))
	for i, lm := range cm.Metrics {
		sorted := make([]analyzer.MetricsSummary, len(lm.Metrics))
		copy(sorted, lm.Metrics)
		slices.SortFunc(sorted, func(a, b analyzer.MetricsSummary) int {
			if a.Package < b.Package {
				return -1
			}

			if a.Package > b.Package {
				return 1
			}

			return 0
		})
		sortedMetrics[i] = analyzer.LanguageMetricsSummary{
			Language: lm.Language,
			Metrics:  sorted,
		}
	}

	return map[string]any{
		"message": cm.Message,
		"metrics": sortedMetrics,
	}
}
