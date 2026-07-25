#!/bin/bash
#
# Fetch benchmark data for Rust analyzer memory benchmarks.
#
# This script downloads a large open-source Rust project (ripgrep) for use
# in benchmark tests. The downloaded repository is gitignored.
#
# Usage:
#   ./fetch.sh              # Download ripgrep (default)
#   ./fetch.sh ripgrep      # Download ripgrep (~50k lines)
#   ./fetch.sh tokio        # Download tokio (~100k+ lines)
#   ./fetch.sh rust-analyzer # Download rust-analyzer (very large)
#
# The project is cloned to ./project_mono for use by the benchmark tests.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

PROJECT="${1:-ripgrep}"
TARGET_DIR="project_mono"

# Repository URLs
declare -A REPOS=(
    ["ripgrep"]="https://github.com/BurntSushi/ripgrep.git"
    ["tokio"]="https://github.com/tokio-rs/tokio.git"
    ["rust-analyzer"]="https://github.com/rust-lang/rust-analyzer.git"
)

# Specific tags/commits for reproducible benchmarks
declare -A REFS=(
    ["ripgrep"]="14.1.1"
    ["tokio"]="tokio-1.40.0"
    ["rust-analyzer"]="2024-10-14"
)

if [[ ! -v "REPOS[$PROJECT]" ]]; then
    echo "Error: Unknown project '$PROJECT'"
    echo "Available projects: ${!REPOS[*]}"
    exit 1
fi

REPO_URL="${REPOS[$PROJECT]}"
REF="${REFS[$PROJECT]}"

echo "=== Rust Analyzer Benchmark Data Fetcher ==="
echo "Project:    $PROJECT"
echo "Repository: $REPO_URL"
echo "Ref:        $REF"
echo "Target:     $TARGET_DIR"
echo ""

# Clean up existing directory if it exists
if [[ -d "$TARGET_DIR" ]]; then
    echo "Removing existing $TARGET_DIR..."
    rm -rf "$TARGET_DIR"
fi

echo "Cloning $PROJECT..."
git clone --depth 1 --branch "$REF" "$REPO_URL" "$TARGET_DIR"

# Remove .git directory to save space (we don't need history)
rm -rf "$TARGET_DIR/.git"

# Count Rust files for verification
RS_COUNT=$(find "$TARGET_DIR" -name "*.rs" | wc -l)
RS_LINES=$(find "$TARGET_DIR" -name "*.rs" -exec cat {} + 2>/dev/null | wc -l)

echo ""
echo "=== Download Complete ==="
echo "Rust files: $RS_COUNT"
echo "Total lines: $RS_LINES"
echo ""
echo "Run benchmarks with:"
echo "  go test -bench=BenchmarkRustAnalyzer -benchmem ./internal/analyzer/rust/..."
