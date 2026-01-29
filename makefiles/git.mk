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

ifndef _include_git_mk
_include_git_mk = 1

include makefiles/shared.mk

.PHONY: git-dirty git-hooks git-submodules

git-dirty: ## Check for uncommited changes
	$(info $(_bullet) Checking for uncommited changes)
	git status --porcelain
	git diff --quiet --exit-code

git-hooks: ## Configure git hooks
	$(info $(_bullet) Configuring git hooks)
	git config core.hooksPath .githooks

git-submodules: ## Update submodules
	$(info $(_bullet) Updating submodules)
	git submodule update --init --recursive

endif
