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

ifndef _include_shared_mk
_include_shared_mk := 1

OS ?= $(shell uname -s | tr [:upper:] [:lower:])
ARCH ?= $(shell uname -m)

ifeq ($(ARCH),x86_64)
	ARCH = amd64
endif

BIN = $(abspath bin)

$(BIN):
	@mkdir -p $(BIN)

.PHONY: help clean deps vendor generate format lint test test-coverage integration-test smoke-test load-test stress-test soak-test build bootrap deploy run dev debug

all: deps generate format lint test build

help: ## Help
	@cat $(sort $(MAKEFILE_LIST)) | grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}' | sort

clean: clean-bin ## Clean targets

deps: ## Download dependencies

vendor: ## Vendor dependencies

generate: ## Generate code

format: ## Format code

lint: ## Lint code

test: ## Run tests

test-coverage: ## Run tests with coverage

integration-test: ## Run integration tests

smoke-test: ## Run smoke tests

load-test: ## Run load tests

stress-test: ## Run stress tests

soak-test: ## Run soak tests

build: ## Build all targets

bootstrap: ## Bootstrap

deploy: ## Deploy

run: ## Run

dev: ## Run in development mode

debug: ## Run in debug mode

.PHONY: clean-bin git-dirty git-hooks

clean-bin: ## Clean installed tools
	$(info $(_bullet) Cleaning <bin>)
	rm -rf bin/

_bullet := $(shell printf "\033[34;1m▶\033[0m")

endif
