package server

import (
	"path/filepath"
	"strings"

	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

// gitignoreFilter answers "should events at this path be ignored?" against the
// repo's full gitignore rule set (nested .gitignore files, .git/info/exclude,
// and the global core.excludesFile). Built once at watcher startup and read
// without synchronization from the event-receive goroutine.
type gitignoreFilter struct {
	repoRoot string
	matcher  gitignore.Matcher
}

func newGitignoreFilter(repoRoot string) (*gitignoreFilter, error) {
	fs := osfs.New(repoRoot)
	patterns, err := gitignore.ReadPatterns(fs, nil)
	if err != nil {
		return nil, err
	}
	return &gitignoreFilter{
		repoRoot: repoRoot,
		matcher:  gitignore.NewMatcher(patterns),
	}, nil
}

// shouldIgnore returns true if the path falls inside repoRoot and matches a
// gitignore rule. Paths outside repoRoot return false (we don't presume to
// ignore foreign events).
func (f *gitignoreFilter) shouldIgnore(path string, isDir bool) bool {
	rel, err := filepath.Rel(f.repoRoot, path)
	if err != nil {
		return false
	}
	if strings.HasPrefix(rel, "..") || rel == "." {
		return false
	}
	segments := strings.Split(filepath.ToSlash(rel), "/")
	return f.matcher.Match(segments, isDir)
}
