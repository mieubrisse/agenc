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

.PHONY: build check clean docs genskill setup test

setup:
	@if git rev-parse --git-dir >/dev/null 2>&1; then \
		current=$$(git config core.hooksPath 2>/dev/null); \
		if [ "$$current" != ".githooks" ]; then \
			git config core.hooksPath .githooks; \
			echo "Git hooks configured (.githooks/)"; \
		fi; \
	fi

check:
	@test -f internal/claudeconfig/prime_content.md || touch internal/claudeconfig/prime_content.md
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
	@echo "✓ Static analysis OK"
	@echo "Running tests..."
	@go test ./...
	@echo "✓ Tests passed"

build: genskill docs setup check
	@echo "Building agenc..."
	@go build -ldflags "$(LDFLAGS)" -o agenc .
	@echo "✓ Build complete"

docs: genskill
	go run ./cmd/gendocs

genskill:
	@# Ensure embed placeholder exists before Go compilation (fresh checkout)
	@test -f internal/claudeconfig/prime_content.md || touch internal/claudeconfig/prime_content.md
	go run ./cmd/genskill

test:
	@echo "Running tests..."
	@go test ./...
	@echo "✓ Tests passed"

clean:
	rm -f agenc
