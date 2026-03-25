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

# Minimum per-package test coverage percentage. Packages below this threshold
# cause `make check` to fail. Ratchet up over time as coverage improves.
# Target: 60%. Current codebase has packages as low as ~7%, so starting at 5%
# to establish the enforcement mechanism. Raise incrementally as tests are added.
COVERAGE_THRESHOLD := 5

# Packages excluded from coverage enforcement (one Go import-path grep pattern
# per line). Entry-point and code-generation packages that contain only a main()
# or are not meaningfully unit-testable belong here.
COVERAGE_EXCLUDE_PATTERNS := \
	github.com/odyssey/agenc$$ \
	/cmd/gendocs$$ \
	/cmd/genprime$$ \
	/internal/version$$

.PHONY: bin build check clean compile docs e2e genprime setup test test-env test-env-clean

setup:
	@if git rev-parse --git-dir >/dev/null 2>&1; then \
		current=$$(git config core.hooksPath 2>/dev/null); \
		if [ "$$current" != ".githooks" ]; then \
			git config core.hooksPath .githooks; \
			echo "Git hooks configured (.githooks/)"; \
		fi; \
	fi

check: genprime
	@echo "Checking module tidiness..."
	@go mod tidy
	@dirty=$$(git diff -- go.mod go.sum); \
	untracked=$$(git ls-files --others --exclude-standard -- go.sum); \
	if [ -n "$$dirty" ] || [ -n "$$untracked" ]; then \
		echo "❌ go.mod or go.sum is not tidy:"; \
		if [ -n "$$dirty" ]; then echo "$$dirty"; fi; \
		if [ -n "$$untracked" ]; then echo "  New file: go.sum"; fi; \
		git checkout -- go.mod go.sum 2>/dev/null || true; \
		echo ""; \
		echo "Run: go mod tidy"; \
		exit 1; \
	fi
	@echo "✓ Modules OK"
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
	@echo "Running govulncheck..."
	@govulncheck ./...
	@echo "✓ Vulncheck OK"
	@echo "Running deadcode analysis..."
	@output=$$(deadcode ./... 2>&1); \
	rc=$$?; \
	if [ "$$rc" -ne 0 ]; then \
		echo "❌ deadcode failed:"; \
		echo "$$output"; \
		exit 1; \
	fi; \
	if [ -n "$$output" ]; then \
		echo "⚠ Dead code found (informational — will become a hard error after cleanup):"; \
		echo "$$output"; \
	fi
	@echo "✓ Deadcode OK"
	@echo "Running tests with coverage..."
	@set -o pipefail; go test -race -coverprofile=coverage.out ./... 2>&1 | tee coverage-test.log
	@echo "✓ Tests passed"
	@echo "Checking per-package coverage (threshold: $(COVERAGE_THRESHOLD)%)..."
	@failed=0; \
	while IFS= read -r line; do \
		pkg=$$(echo "$$line" | awk '{for(i=1;i<=NF;i++) if($$i ~ /^github\.com\//) {print $$i; exit}}'); \
		if [ -z "$$pkg" ]; then continue; fi; \
		skip=0; \
		for pat in $(COVERAGE_EXCLUDE_PATTERNS); do \
			if echo "$$pkg" | grep -qE "$$pat"; then \
				skip=1; \
				break; \
			fi; \
		done; \
		if [ "$$skip" = "1" ]; then continue; fi; \
		if echo "$$line" | grep -q '\[no test files\]'; then \
			echo "  ✗ $$pkg: no test files"; \
			failed=1; \
			continue; \
		fi; \
		pct=$$(echo "$$line" | grep -oE '[0-9]+\.[0-9]+%' | tr -d '%'); \
		if [ -z "$$pct" ]; then continue; fi; \
		if [ "$$(echo "$$pct < $(COVERAGE_THRESHOLD)" | bc)" = "1" ]; then \
			echo "  ✗ $$pkg: $${pct}% < $(COVERAGE_THRESHOLD)%"; \
			failed=1; \
		fi; \
	done < coverage-test.log; \
	rm -f coverage.out coverage-test.log; \
	if [ "$$failed" = "1" ]; then \
		echo "❌ Some packages are below the $(COVERAGE_THRESHOLD)% coverage threshold"; \
		exit 1; \
	fi
	@echo "✓ Coverage OK"

compile:
	@echo "Building agenc..."
	@mkdir -p $(BUILD_DIR)
	@go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/agenc .
	@# Create wrapper script that sets test-env variables
	@printf '#!/usr/bin/env bash\nset -euo pipefail\nscript_dirpath="$$(cd "$$(dirname "$${0}")" && pwd)"\nexport AGENC_DIRPATH="$$(cd "$${script_dirpath}/../$(TEST_ENV_DIR)" 2>/dev/null && pwd || echo "$${script_dirpath}/../$(TEST_ENV_DIR)")"\nexport AGENC_TEST_ENV=1\nexec "$${script_dirpath}/agenc" "$$@"\n' > $(BUILD_DIR)/agenc-test
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
	@echo "Running tests with coverage..."
	@go test -race -cover ./...
	@echo "✓ Tests passed"

e2e: bin test-env
	@scripts/e2e-test.sh; rc=$$?; $(MAKE) test-env-clean; exit $$rc

test-env:
	@echo "Creating test environment at $(TEST_ENV_DIR)/..."
	@mkdir -p $(TEST_ENV_DIR)/config
	@cd $(TEST_ENV_DIR)/config && git init --quiet 2>/dev/null || true
	@# Copy OAuth token from the real installation so missions can authenticate
	@real_token="$${HOME}/.agenc/cache/oauth-token"; \
	if [ -f "$${real_token}" ]; then \
		mkdir -p $(TEST_ENV_DIR)/cache; \
		cp "$${real_token}" $(TEST_ENV_DIR)/cache/oauth-token; \
		echo "  Copied OAuth token from ~/.agenc"; \
	else \
		echo "  ⚠ No OAuth token found at ~/.agenc/cache/oauth-token — missions will prompt for auth"; \
	fi
	@# Write namespace hash so agents know their isolated namespace
	@test_env_abs="$$(cd "$(CURDIR)/$(TEST_ENV_DIR)" && pwd)"; \
	ns_hash=$$(printf '%s' "$${test_env_abs}" | shasum -a 256 | cut -c1-8); \
	echo "$${ns_hash}" > $(TEST_ENV_DIR)/namespace; \
	echo "  Namespace: agenc-$${ns_hash}"
	@echo "✓ Test environment ready"
	@echo "  Run with: $(BUILD_DIR)/agenc-test"

test-env-clean:
	@echo "Tearing down test environment..."
	@# Stop the server: prefer agenc-test binary, fall back to direct PID kill
	@if [ -x "$(BUILD_DIR)/agenc-test" ] && [ -f "$(TEST_ENV_DIR)/server/server.pid" ]; then \
		"$(BUILD_DIR)/agenc-test" server stop 2>/dev/null || true; \
	elif [ -f "$(TEST_ENV_DIR)/server/server.pid" ]; then \
		pid=$$(cat "$(TEST_ENV_DIR)/server/server.pid" 2>/dev/null); \
		if [ -n "$${pid}" ] && kill -0 "$${pid}" 2>/dev/null; then \
			kill "$${pid}" 2>/dev/null || true; \
			echo "  Killed server process $${pid} (binary not available for graceful stop)"; \
		fi; \
	fi
	@# Kill namespaced tmux sessions (agenc-HASH and agenc-HASH-pool).
	@# Read the hash from the namespace file if available, otherwise compute it.
	@if [ -f "$(TEST_ENV_DIR)/namespace" ]; then \
		pool_hash=$$(cat "$(TEST_ENV_DIR)/namespace"); \
	else \
		test_env_abs="$$(cd "$(CURDIR)/$(TEST_ENV_DIR)" 2>/dev/null && pwd || echo "$(CURDIR)/$(TEST_ENV_DIR)")"; \
		pool_hash=$$(printf '%s' "$${test_env_abs}" | shasum -a 256 | cut -c1-8); \
	fi; \
	tmux kill-session -t "=agenc-$${pool_hash}" 2>/dev/null || true; \
	tmux kill-session -t "=agenc-$${pool_hash}-pool" 2>/dev/null || true
	@rm -rf $(TEST_ENV_DIR)
	@echo "✓ Test environment removed"

clean:
	rm -rf $(BUILD_DIR)
