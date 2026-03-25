VERSION_PKG := github.com/odyssey/agenc/internal/version

# Determine version from git state:
#   1. If HEAD is tagged AND working tree is clean → use the tag
#   2. If working tree is clean (no tag) → use the short commit hash
#   3. If working tree is dirty → use short commit hash + "-dirty"
GIT_DIRTY := $(shell git diff --quiet 2>/dev/null && echo clean || echo dirty)
GIT_HASH  := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
GIT_TAG   := $(shell git describe --tags --exact-match HEAD 2>/dev/null)

ifeq ($(GIT_DIRTY),clean)
  ifneq ($(GIT_TAG),)
    VERSION := $(GIT_TAG)
  else
    VERSION := $(GIT_HASH)
  endif
else
  VERSION := $(GIT_HASH)-dirty
endif

LDFLAGS := -X $(VERSION_PKG).Version=$(VERSION)

TEST_ENV_DIR := _test-env
BUILD_DIR    := _build

.PHONY: bin build check clean compile docs genprime setup test test-env test-env-clean

setup:
	@if git rev-parse --git-dir >/dev/null 2>&1; then \
		current=$$(git config core.hooksPath 2>/dev/null); \
		if [ "$$current" != ".githooks" ]; then \
			git config core.hooksPath .githooks; \
			echo "Git hooks configured (.githooks/)"; \
		fi; \
	fi

check: genprime
	@echo "Checking code formatting..."
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "❌ Files need formatting:"; \
		echo "$$unformatted"; \
		echo ""; \
		echo "Run: gofmt -w ."; \
		exit 1; \
	fi
	@echo "✓ Formatting OK"
	@echo "Running go vet..."
	@go vet ./...
	@echo "✓ Vet OK"
	@echo "Running golangci-lint..."
	@golangci-lint run ./...
	@echo "✓ Lint OK"
	@echo "Running tests..."
	@go test ./...
	@echo "✓ Tests passed"

compile:
	@echo "Building agenc..."
	@mkdir -p $(BUILD_DIR)
	@go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/agenc .
	@# Create wrapper script that sets test-env variables
	@printf '#!/usr/bin/env bash\nset -euo pipefail\nscript_dirpath="$$(cd "$$(dirname "$${0}")" && pwd)"\nexport AGENC_DIRPATH="$${script_dirpath}/../$(TEST_ENV_DIR)"\nexport AGENC_TEST_ENV=1\nexec "$${script_dirpath}/agenc" "$$@"\n' > $(BUILD_DIR)/agenc-test
	@chmod +x $(BUILD_DIR)/agenc-test
	@echo "✓ Build complete ($(BUILD_DIR)/agenc, $(BUILD_DIR)/agenc-test)"

bin: genprime compile

build: genprime docs setup check compile

docs: genprime
	go run ./cmd/gendocs

genprime:
	@# Ensure embed placeholder exists before Go compilation (fresh checkout)
	@test -f internal/claudeconfig/prime_content.md || touch internal/claudeconfig/prime_content.md
	go run ./cmd/genprime

test:
	@echo "Running tests..."
	@go test ./...
	@echo "✓ Tests passed"

test-env:
	@echo "Creating test environment at $(TEST_ENV_DIR)/..."
	@mkdir -p $(TEST_ENV_DIR)/config
	@cd $(TEST_ENV_DIR)/config && git init --quiet 2>/dev/null || true
	@echo "✓ Test environment ready"
	@echo "  Run with: $(BUILD_DIR)/agenc-test"

test-env-clean:
	@echo "Removing test environment..."
	@rm -rf $(TEST_ENV_DIR)
	@echo "✓ Test environment removed"

clean:
	rm -rf $(BUILD_DIR)
