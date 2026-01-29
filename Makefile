# uda Project Makefile

# Build configuration
GO_SRC_DIR ?= .
GOLANGCILINT_CONFIG_PATH ?= $(PWD)/.golangci.yml
CGO_ENABLED = 1
CGO_LDFLAGS = "-ldl"

# Include modular makefiles (following template pattern)
include makefiles/shared.mk
include makefiles/go.mk
include makefiles/git.mk

.PHONY: build uda pr-ready verify 

# Add uda to main build target
build: uda

uda: ## Build the uda CLI binary
	$(info $(_bullet) Building <uda>)
	@cd $(GO_SRC_DIR) && \
	CGO_ENABLED=$(CGO_ENABLED) CGO_LDFLAGS=$(CGO_LDFLAGS) \
	go build -o ../bin/uda ./cmd/uda

pr-ready: tidy-go generate format build lint test git-dirty ## Run comprehensive pre-commit checks

# Verify staged changes and record tree SHA for pre-commit hook
# Run this before committing to validate tests pass on staged changes
verify: pr-ready ## Verify staged changes pass all checks and record for commit
	$(info $(_bullet) Recording verified tree SHA)
	@git write-tree > .git/verified-tree
	@echo "Verified tree: $$(cat .git/verified-tree)"
	@echo "You can now commit your changes."
