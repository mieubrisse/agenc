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

.PHONY: build clean docs

build: docs
	go build -ldflags "$(LDFLAGS)" -o agenc .

docs:
	go run ./cmd/gendocs

clean:
	rm -f agenc
