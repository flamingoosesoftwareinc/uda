# uda

CGO_ENABLED = 1
CGO_LDFLAGS = "-ldl"
GO_ENV = CGO_ENABLED=$(CGO_ENABLED) CGO_LDFLAGS=$(CGO_LDFLAGS)

.PHONY: help build install test fmt tidy

help: ## List targets
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'

build: ## Build the uda binary into ./bin
	$(GO_ENV) go build -o ./bin/uda

install: ## Install uda into GOBIN
	$(GO_ENV) go install .

test: ## Run all tests
	$(GO_ENV) go test ./...

fmt: ## Format
	go fmt ./...

tidy: ## Tidy modules
	go mod tidy
