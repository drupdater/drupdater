package workflow

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/drupdater/drupdater/internal/repo"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/format/index"
)

// Committer is how an addon gets its changes onto the update branch: it names the paths it
// changed, and the runner does the git.
type Committer interface {
	// Commit stages paths, files or directories, deletions included, and commits them alone. It
	// reports false without committing when they hold no change.
	Commit(message string, paths ...string) (bool, error)
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

func (c *gitCommitter) Commit(message string, paths ...string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, p := range paths {
		// A path that neither exists nor is tracked has nothing to stage.
		if _, err := c.worktree.Add(p); err != nil && !errors.Is(err, index.ErrEntryNotFound) {
			return false, fmt.Errorf("failed to stage %s: %w", p, err)
		}
	}

	status, err := c.worktree.Status()
	if err != nil {
		return false, fmt.Errorf("failed to read worktree status: %w", err)
	}
	staged := false
	var foreign []string
	for file, s := range status {
		if s.Staging == git.Unmodified || s.Staging == git.Untracked {
			continue
		}
		if !slices.ContainsFunc(paths, func(p string) bool { return pathWithin(file, p) }) {
			foreign = append(foreign, file)
			continue
		}
		staged = true
	}
	// Refused rather than committed, or this commit would carry another change under its message.
	if len(foreign) > 0 {
		slices.Sort(foreign)
		return false, fmt.Errorf("refusing to commit %q: %s already staged", message, strings.Join(foreign, ", "))
	}
	if !staged {
		return false, nil
	}

	if _, err := c.worktree.Commit(message, commitOptions(c.author)); err != nil {
		return false, fmt.Errorf("failed to commit %q: %w", message, err)
	}
	return true, nil
}

// commitOptions signs a commit explicitly, so no identity has to be written into git config.
func commitOptions(author repo.Identity) *git.CommitOptions {
	signature := author.Signature()
	return &git.CommitOptions{Author: signature, Committer: signature}
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
