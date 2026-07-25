// Package cache provides a filesystem-backed XDG cache for per-commit analysis artifacts.
package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
)

// Config holds cache configuration.
type Config struct {
	BaseDir string // Default: $HOME/.cache/uda
}

// Cache manages the metrics cache.
type Cache struct {
	config Config
}

// NewCache creates a new cache with the given configuration.
// If BaseDir is empty, it defaults to $HOME/.cache/uda.
func NewCache(cfg Config) *Cache {
	if cfg.BaseDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}

		cfg.BaseDir = filepath.Join(home, ".cache", "uda")
	}

	return &Cache{config: cfg}
}

// Path returns the path to the cached metrics file for a given repo and commit.
func (c *Cache) Path(repoID, sha string) string {
	return filepath.Join(c.config.BaseDir, repoID, sha, "metrics.json")
}

// Has returns true if the cache contains metrics for the given repo and commit.
func (c *Cache) Has(repoID, sha string) bool {
	_, err := os.Stat(c.Path(repoID, sha))

	return err == nil
}

// Get retrieves cached metrics for the given repo and commit.
// Returns an error if the cache entry does not exist.
func (c *Cache) Get(repoID, sha string) ([]analyzer.LanguageMetricsSummary, error) {
	data, err := os.ReadFile(c.Path(repoID, sha))
	if err != nil {
		return nil, fmt.Errorf("reading cache: %w", err)
	}

	var metrics []analyzer.LanguageMetricsSummary
	if err := json.Unmarshal(data, &metrics); err != nil {
		return nil, fmt.Errorf("unmarshaling cache: %w", err)
	}

	return metrics, nil
}

// Put stores metrics in the cache for the given repo and commit.
func (c *Cache) Put(repoID, sha string, metrics []analyzer.LanguageMetricsSummary) error {
	path := c.Path(repoID, sha)
	dir := filepath.Dir(path)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating cache directory: %w", err)
	}

	data, err := json.MarshalIndent(metrics, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling metrics: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing cache: %w", err)
	}

	return nil
}

// WorkspacePath returns the path to the workspace directory for a given repo.
func (c *Cache) WorkspacePath(repoID string) string {
	return filepath.Join(c.config.BaseDir, repoID, "workspace")
}
