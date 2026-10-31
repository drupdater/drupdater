package workflow

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/drupdater/drupdater/internal/repo"
)

// Committer is how an addon gets its changes onto the update branch: it names the paths it
// changed, and the runner does the git.
type Committer interface {
	// Commit stages paths, files or directories, deletions included, and commits them alone. It
	// reports false without committing when they hold no change.
	Commit(ctx context.Context, message string, paths ...string) (bool, error)
}

type gitCommitter struct {
	worktree Worktree
	author   repo.Identity
	// Sites update concurrently and addons commit from their events, all into one index.
	mu sync.Mutex
}

func newGitCommitter(worktree Worktree, author repo.Identity) *gitCommitter {
	return &gitCommitter{worktree: worktree, author: author}
}

func (c *gitCommitter) Commit(ctx context.Context, message string, paths ...string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.worktree.Stage(ctx, paths...); err != nil {
		return false, fmt.Errorf("failed to stage %s: %w", strings.Join(paths, ", "), err)
	}

	staged, err := c.worktree.Staged(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to read the staged changes: %w", err)
	}
	foreign := slices.DeleteFunc(slices.Clone(staged), func(file string) bool {
		return slices.ContainsFunc(paths, func(p string) bool { return pathWithin(file, p) })
	})
	// Refused rather than committed, or this commit would carry another change under its message.
	if len(foreign) > 0 {
		slices.Sort(foreign)
		return false, fmt.Errorf("refusing to commit %q: %s already staged", message, strings.Join(foreign, ", "))
	}
	if len(staged) == 0 {
		return false, nil
	}

	if err := c.worktree.Commit(ctx, message, c.author); err != nil {
		return false, fmt.Errorf("failed to commit %q: %w", message, err)
	}
	return true, nil
}

// pathWithin reports whether filePath is dir or lives under it — not a substring test, which
// would match the sibling "translations-backup/de.po" for dir "translations". Empty dir matches all.
func pathWithin(filePath string, dir string) bool {
	dir = strings.Trim(filepath.ToSlash(dir), "/")
	if dir == "" {
		return true
	}
	filePath = strings.Trim(filepath.ToSlash(filePath), "/")
	return filePath == dir || strings.HasPrefix(filePath, dir+"/")
}
