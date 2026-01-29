#!/bin/bash
# Setup git hooks for the repository
# Run this script to install pre-commit and other hooks

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
HOOKS_DIR="$REPO_ROOT/.git/hooks"

echo "Setting up git hooks..."

# Ensure hooks directory exists
mkdir -p "$HOOKS_DIR"

# Install pre-commit hook
# The hook verifies that `make verify` was run on the staged changes
# This avoids git index contention by running tests before the commit
cp "$SCRIPT_DIR/pre-commit" "$HOOKS_DIR/pre-commit"
chmod +x "$HOOKS_DIR/pre-commit"
echo "Installed pre-commit hook"

echo "Git hooks setup complete."
