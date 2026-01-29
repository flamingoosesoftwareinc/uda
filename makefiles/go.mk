# MIT License
#
# Copyright (c) 2020 Zbigniew Mandziejewicz
#
# Permission is hereby granted, free of charge, to any person obtaining a copy
# of this software and associated documentation files (the "Software"), to deal
# in the Software without restriction, including without limitation the rights
# to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
# copies of the Software, and to permit persons to whom the Software is
# furnished to do so, subject to the following conditions:
# 
# The above copyright notice and this permission notice shall be included in all
# copies or substantial portions of the Software.
# 
# THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
# IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
# FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
# AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
# LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
# OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
# SOFTWARE.

ifndef _include_go_mk
_include_go_mk = 1

include makefiles/shared.mk

GO ?= go
FORMAT_FILES ?= .
GO_SRC_DIR ?= .

GOLANGCILINT := $(BIN)/golangci-lint
GOLANGCILINT_VERSION ?= v2.8.0
GOLANGCILINT_CONCURRENCY ?= 16
GOLANGCILINT_CONFIG_PATH ?= $(GO_SRC_DIR)/.golangci.yml

GOWRAP := $(BIN)/gowrap
GOWRAP_VERSION ?= v1.4.0

$(GOLANGCILINT): | $(BIN)
	$(info $(_bullet) Installing <golangci-lint>)
	GOBIN=$(BIN) $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCILINT_VERSION)

$(GOWRAP): | $(BIN)
	$(info $(_bullet) Installing <gowrap>)
	GOBIN=$(BIN) $(GO) install github.com/hexdigest/gowrap/cmd/gowrap@$(GOWRAP_VERSION)

clean: clean-go

deps: deps-go

vendor: vendor-go

format: format-go

lint: lint-go

test: test-go

generate: generate-go

test-coverage: test-coverage-go

integration-test: integration-test-go

.PHONY: deps-go format-go lint-go test-go test-coverage-go integration-test-go update-golden-go tidy-go generate-go

clean-go: ## Clean Go
	$(info $(_bullet) Cleaning <go>)
	cd $(GO_SRC_DIR) && rm -rf vendor/

deps-go: ## Download Go dependencies
	$(info $(_bullet) Downloading dependencies <go>)
	cd $(GO_SRC_DIR) && $(GO) mod download

vendor-go: ## Vendor Go dependencies
	$(info $(_bullet) Vendoring dependencies <go>)
	cd $(GO_SRC_DIR) && $(GO) mod vendor

format-go: $(GOLANGCILINT) ## Format Go code
	$(info $(_bullet) Formatting code)
	cd $(GO_SRC_DIR) && $(GOLANGCILINT) fmt --config $(GOLANGCILINT_CONFIG_PATH)

lint-go: $(GOLANGCILINT)
	$(info $(_bullet) Linting <go>) 
	cd $(GO_SRC_DIR) && $(GOLANGCILINT) run --fix --config $(GOLANGCILINT_CONFIG_PATH) --concurrency $(GOLANGCILINT_CONCURRENCY) ./...

test-go: ## Run Go tests
	$(info $(_bullet) Running tests <go>)
	cd $(GO_SRC_DIR) && \
	CGO_ENABLED=$(CGO_ENABLED) CGO_LDFLAGS=$(CGO_LDFLAGS) \
	$(GO) test ./...
	
test-coverage-go: ## Run Go tests with coverage
	$(info $(_bullet) Running tests with coverage <go>) 
	cd $(GO_SRC_DIR) && \
	CGO_ENABLED=$(CGO_ENABLED) CGO_LDFLAGS=$(CGO_LDFLAGS) \
	$(GO) test -cover ./...

integration-test-go: ## Run Go integration tests
	$(info $(_bullet) Running integration tests <go>)
	cd $(GO_SRC_DIR) && \
	CGO_ENABLED=$(CGO_ENABLED) CGO_LDFLAGS=$(CGO_LDFLAGS) \
	$(GO) test -tags integration -count 1 ./...

update-golden-go: ## Update golden test files
	$(info $(_bullet) Updating golden files <go>)
	cd $(GO_SRC_DIR) && \
	CGO_ENABLED=$(CGO_ENABLED) CGO_LDFLAGS=$(CGO_LDFLAGS) \
	UPDATE_GOLDEN=1 $(GO) test ./...

tidy-go: ## Tidy Go modules
	$(info $(_bullet) Tidying modules <go>)
	cd $(GO_SRC_DIR) && $(GO) mod tidy

generate-go: $(GOWRAP) ## Run go generate
	$(info $(_bullet) Running go generate <go>)
	cd $(GO_SRC_DIR) && PATH="$(BIN):$(PATH)" $(GO) generate ./...

endif
