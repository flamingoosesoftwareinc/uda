// Package history analyzes coupling/instability metrics across a sequence of git commits.
package history

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"time"

	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer/golang"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer/rust"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer/typescript"
	"github.com/flamingoosesoftwareinc/uda/internal/cache"
	"github.com/flamingoosesoftwareinc/uda/internal/detect"
	udagit "github.com/flamingoosesoftwareinc/uda/internal/git"
	"github.com/go-git/go-git/v5/plumbing"
)

// CommitMetrics holds metrics for a single commit.
type CommitMetrics struct {
	SHA       string                            `json:"sha"`
	Timestamp time.Time                         `json:"timestamp"`
	Message   string                            `json:"message"`
	Metrics   []analyzer.LanguageMetricsSummary `json:"metrics"`
}

// Analyzer analyzes metrics across git history.
type Analyzer struct {
	repo      *udagit.Repository
	cache     *cache.Cache
	language  string
	workspace *Workspace
	w         io.Writer
}

// NewAnalyzer creates a new history analyzer.
func NewAnalyzer(
	repo *udagit.Repository,
	cache *cache.Cache,
	language string,
) *Analyzer {
	return &Analyzer{
		repo:     repo,
		cache:    cache,
		language: language,
		w:        os.Stderr,
	}
}

// AnalyzeCommit analyzes a single commit and returns its metrics.
// Uses the cache if available.
func (h *Analyzer) AnalyzeCommit(
	ctx context.Context,
	sha plumbing.Hash,
) (*CommitMetrics, error) {
	shaStr := sha.String()
	repoID := h.repo.RepoID()

	// Get commit info
	commit, err := h.repo.Repo().CommitObject(sha)
	if err != nil {
		return nil, fmt.Errorf("getting commit: %w", err)
	}

	// Check cache
	if h.cache.Has(repoID, shaStr) {
		metrics, err := h.cache.Get(repoID, shaStr)
		if err == nil {
			return &CommitMetrics{
				SHA:       shaStr,
				Timestamp: commit.Author.When,
				Message:   firstLine(commit.Message),
				Metrics:   metrics,
			}, nil
		}
		// Cache read failed, continue with analysis
	}

	// Ensure workspace exists
	if h.workspace == nil {
		workspacePath := h.cache.WorkspacePath(repoID)

		ws, err := NewWorkspace(h.repo, workspacePath)
		if err != nil {
			return nil, fmt.Errorf("creating workspace: %w", err)
		}

		h.workspace = ws
	}

	// Checkout the commit
	if err := h.workspace.Checkout(sha); err != nil {
		return nil, fmt.Errorf("checkout: %w", err)
	}

	// Run analysis
	dirFS := h.workspace.FS()

	analyzers, err := h.selectAnalyzers(ctx, dirFS)
	if err != nil {
		return nil, fmt.Errorf("selecting analyzers: %w", err)
	}

	metrics, err := h.runAnalysis(ctx, analyzers, dirFS)
	if err != nil {
		return nil, fmt.Errorf("running analysis: %w", err)
	}

	// Cache result
	if err := h.cache.Put(repoID, shaStr, metrics); err != nil {
		// Log but don't fail on cache write error
		_, _ = fmt.Fprintf(h.w, "warning: failed to cache metrics: %v\n", err)
	}

	return &CommitMetrics{
		SHA:       shaStr,
		Timestamp: commit.Author.When,
		Message:   firstLine(commit.Message),
		Metrics:   metrics,
	}, nil
}

// AnalyzeRange analyzes a range of commits and returns metrics for each.
func (h *Analyzer) AnalyzeRange(
	ctx context.Context,
	cr *udagit.CommitRange,
) ([]CommitMetrics, error) {
	commits, err := h.repo.Commits(cr)
	if err != nil {
		return nil, fmt.Errorf("getting commits: %w", err)
	}

	results := make([]CommitMetrics, 0, len(commits))
	for _, sha := range commits {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}

		cm, err := h.AnalyzeCommit(ctx, sha)
		if err != nil {
			return nil, fmt.Errorf("analyzing commit %s: %w", sha.String()[:8], err)
		}

		results = append(results, *cm)
	}

	return results, nil
}

// LanguageMetrics holds full metrics for a language, including detailed coupling stats.
type LanguageMetrics struct {
	Language string             `json:"language"`
	Metrics  []analyzer.Metrics `json:"metrics"`
}

// AnalyzeCommitFull analyzes a commit and returns full metrics with dependency details.
// This is more expensive than AnalyzeCommit as it doesn't cache and returns full data.
func (h *Analyzer) AnalyzeCommitFull(
	ctx context.Context,
	sha string,
) ([]LanguageMetrics, error) {
	hash := plumbing.NewHash(sha)

	// Ensure workspace exists
	if h.workspace == nil {
		workspacePath := h.cache.WorkspacePath(h.repo.RepoID())

		ws, err := NewWorkspace(h.repo, workspacePath)
		if err != nil {
			return nil, fmt.Errorf("creating workspace: %w", err)
		}

		h.workspace = ws
	}

	// Checkout the commit
	if err := h.workspace.Checkout(hash); err != nil {
		return nil, fmt.Errorf("checkout: %w", err)
	}

	// Run analysis
	dirFS := h.workspace.FS()

	analyzers, err := h.selectAnalyzers(ctx, dirFS)
	if err != nil {
		return nil, fmt.Errorf("selecting analyzers: %w", err)
	}

	results := make([]LanguageMetrics, 0, len(analyzers))
	for _, langAnalyzer := range analyzers {
		metrics, err := langAnalyzer.Analyze(ctx, dirFS)
		if err != nil {
			return nil, err
		}

		results = append(results, LanguageMetrics{
			Language: langAnalyzer.Name(),
			Metrics:  metrics,
		})
	}

	return results, nil
}

// CheckoutWorkspace ensures the workspace is checked out to the given SHA
// and returns the filesystem path to the workspace.
func (h *Analyzer) CheckoutWorkspace(sha string) (string, error) {
	hash := plumbing.NewHash(sha)

	// Ensure workspace exists
	if h.workspace == nil {
		workspacePath := h.cache.WorkspacePath(h.repo.RepoID())

		ws, err := NewWorkspace(h.repo, workspacePath)
		if err != nil {
			return "", fmt.Errorf("creating workspace: %w", err)
		}

		h.workspace = ws
	}

	// Checkout the commit
	if err := h.workspace.Checkout(hash); err != nil {
		return "", fmt.Errorf("checkout: %w", err)
	}

	return h.workspace.Path(), nil
}

func (h *Analyzer) selectAnalyzers(
	ctx context.Context,
	dir fs.FS,
) ([]analyzer.Analyzer, error) {
	switch h.language {
	case "go":
		return []analyzer.Analyzer{golang.GoAnalyzer()}, nil
	case "rust":
		return []analyzer.Analyzer{rust.RustAnalyzer()}, nil
	case "typescript":
		return []analyzer.Analyzer{typescript.TsAnalyzer()}, nil
	case "auto":
		return h.detectAnalyzers(ctx, dir)
	default:
		return nil, fmt.Errorf("unsupported language: %s", h.language)
	}
}

func (h *Analyzer) detectAnalyzers(
	ctx context.Context,
	dir fs.FS,
) ([]analyzer.Analyzer, error) {
	langs, err := detect.Languages(ctx, dir)
	if err != nil {
		return nil, err
	}

	type entry struct {
		name     string
		analyzer analyzer.Analyzer
	}

	registry := []entry{
		{"go", golang.GoAnalyzer()},
		{"rust", rust.RustAnalyzer()},
		{"typescript", typescript.TsAnalyzer()},
	}
	enryToEntry := map[string]string{
		"Go":         "go",
		"Rust":       "rust",
		"TypeScript": "typescript",
		"TSX":        "typescript",
	}

	detected := make(map[string]struct{})

	for _, lang := range langs {
		if name, ok := enryToEntry[lang]; ok {
			detected[name] = struct{}{}
		}
	}

	var analyzers []analyzer.Analyzer

	for _, e := range registry {
		if _, ok := detected[e.name]; ok {
			analyzers = append(analyzers, e.analyzer)
		}
	}

	if len(analyzers) == 0 {
		return nil, errors.New("could not detect project language; use --language flag")
	}

	return analyzers, nil
}

func (h *Analyzer) runAnalysis(
	ctx context.Context,
	analyzers []analyzer.Analyzer,
	dirFS fs.FS,
) ([]analyzer.LanguageMetricsSummary, error) {
	summaries := make([]analyzer.LanguageMetricsSummary, 0, len(analyzers))

	for _, langAnalyzer := range analyzers {
		metrics, err := langAnalyzer.Analyze(ctx, dirFS)
		if err != nil {
			return nil, err
		}

		metricsSummary := make([]analyzer.MetricsSummary, 0, len(metrics))
		for _, m := range metrics {
			metricsSummary = append(metricsSummary, analyzer.MetricsSummary{
				Package:     string(m.Package),
				Inward:      int(m.InwardCoupling()),
				Outward:     int(m.OutwardCoupling()),
				Instability: m.Instability(),
			})
		}

		summaries = append(summaries, analyzer.LanguageMetricsSummary{
			Language: langAnalyzer.Name(),
			Metrics:  metricsSummary,
		})
	}

	return summaries, nil
}

func firstLine(s string) string {
	for i, c := range s {
		if c == '\n' {
			return s[:i]
		}
	}

	return s
}
