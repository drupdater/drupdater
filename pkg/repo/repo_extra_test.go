package repo

import (
	"os"
	"path/filepath"
	"testing"

	git "github.com/go-git/go-git/v5"
	gitConfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestPathWithin(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		dir      string
		expected bool
	}{
		{name: "file directly in the directory", filePath: "translations/de.po", dir: "translations", expected: true},
		{name: "file nested deeper in the directory", filePath: "translations/custom/de.po", dir: "translations", expected: true},
		{name: "the directory itself", filePath: "translations", dir: "translations", expected: true},
		{name: "an empty dir matches everything", filePath: "any/file.txt", dir: "", expected: true},
		{name: "unrelated directory", filePath: "modules/custom/x.php", dir: "translations", expected: false},
		// The reason pathWithin exists: a substring test would call these a match.
		{name: "sibling sharing a name prefix", filePath: "translations-backup/de.po", dir: "translations", expected: false},
		{name: "prefix match inside a longer segment", filePath: "web/translationsx/de.po", dir: "web/translations", expected: false},
		{name: "leading and trailing slashes are ignored", filePath: "/translations/de.po", dir: "translations/", expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, pathWithin(tt.filePath, tt.dir))
		})
	}
}

func TestIsSomethingStagedInPathIgnoresPrefixSiblings(t *testing.T) {
	service := NewGitRepositoryService(zap.NewNop())

	// The translations addon commits on this answer, so a false positive from the sibling
	// "translations-backup" creates a commit with nothing in it.
	worktree := NewMockWorktree(t)
	worktree.EXPECT().Status().Return(git.Status{
		"translations-backup/de.po": &git.FileStatus{Staging: git.Modified},
	}, nil)

	assert.False(t, service.IsSomethingStagedInPath(worktree, "translations"))
}

// initRepoWithCommit creates a repo containing a single empty commit.
func initRepoWithCommit(t *testing.T) (string, *git.Repository) {
	t.Helper()
	dir := t.TempDir()
	r, err := git.PlainInit(dir, false)
	require.NoError(t, err)
	wt, err := r.Worktree()
	require.NoError(t, err)
	_, err = wt.Commit("init", &git.CommitOptions{
		AllowEmptyCommits: true,
		Author:            &object.Signature{Name: "t", Email: "t@example.com"},
	})
	require.NoError(t, err)
	return dir, r
}

func TestOpenRepositoryRemovesPrepareCommitMsgHook(t *testing.T) {
	service := NewGitRepositoryService(zap.NewNop())

	dir, _ := initRepoWithCommit(t)
	hookPath := filepath.Join(dir, ".git", "hooks", "prepare-commit-msg")
	require.NoError(t, os.MkdirAll(filepath.Dir(hookPath), 0o755))
	require.NoError(t, os.WriteFile(hookPath, []byte("#!/bin/sh\nexit 1\n"), 0o600))

	_, worktree, _, err := service.OpenRepository(dir, "Bot", "bot@example.com")
	require.NoError(t, err)
	assert.NotNil(t, worktree)

	// The hook is removed because it cannot be bypassed with --no-verify.
	assert.NoFileExists(t, hookPath)
}

func TestGetRemoteURLFallsBackForSCPStyleURLs(t *testing.T) {
	service := NewGitRepositoryService(zap.NewNop())

	dir := t.TempDir()
	r, err := git.PlainInit(dir, false)
	require.NoError(t, err)
	// url.Parse rejects the SCP form ("first path segment in URL cannot contain colon"), so
	// the raw URL has to be returned unchanged rather than dropped.
	_, err = r.CreateRemote(&gitConfig.RemoteConfig{
		Name: "origin",
		URLs: []string{"git@github.com:drupdater/drupdater.git"},
	})
	require.NoError(t, err)

	url, err := service.GetRemoteURL(dir)
	require.NoError(t, err)
	assert.Equal(t, "git@github.com:drupdater/drupdater.git", url)
}

func TestGetRemoteURLErrorsOnNonRepository(t *testing.T) {
	service := NewGitRepositoryService(zap.NewNop())

	_, err := service.GetRemoteURL(t.TempDir())
	require.Error(t, err)
	// The cause must survive the wrapper, so callers can tell "not a repository" apart from
	// "no origin" rather than matching on message text.
	require.ErrorIs(t, err, git.ErrRepositoryNotExists)
}

func TestGetRemoteURLErrorsWhenOriginMissing(t *testing.T) {
	service := NewGitRepositoryService(zap.NewNop())

	dir := t.TempDir()
	_, err := git.PlainInit(dir, false)
	require.NoError(t, err)

	_, err = service.GetRemoteURL(dir)
	require.Error(t, err)
	require.ErrorIs(t, err, git.ErrRemoteNotFound)
}

func TestOpenRepositoryErrorsOnNonRepository(t *testing.T) {
	service := NewGitRepositoryService(zap.NewNop())

	_, _, _, err := service.OpenRepository(t.TempDir(), "Bot", "bot@example.com")
	require.Error(t, err)
	require.ErrorIs(t, err, git.ErrRepositoryNotExists)
}

func TestIsShallowCloneErrorsOnNonRepository(t *testing.T) {
	service := NewGitRepositoryService(zap.NewNop())

	_, err := service.IsShallowClone(t.TempDir())
	require.Error(t, err)
	require.ErrorIs(t, err, git.ErrRepositoryNotExists)
}

func TestCloneRepository(t *testing.T) {
	service := NewGitRepositoryService(zap.NewNop())

	t.Run("clones a branch and prepares the checkout", func(t *testing.T) {
		source, r := initRepoWithCommit(t)
		head, err := r.Head()
		require.NoError(t, err)

		repository, worktree, path, err := service.CloneRepository(source, head.Name().Short(), "", "Bot", "bot@example.com")
		require.NoError(t, err)
		assert.NotNil(t, repository)
		assert.NotNil(t, worktree)
		require.NotEmpty(t, path)

		// The clone lives under a per-URL directory in the temp dir, which is what lets the
		// workflow's cleanup remove just this run's clone.
		assert.True(t, filepath.IsAbs(path))
		assert.DirExists(t, filepath.Join(path, ".git"))

		cloned, err := git.PlainOpen(path)
		require.NoError(t, err)
		cfg, err := cloned.Config()
		require.NoError(t, err)
		assert.Equal(t, "Bot", cfg.User.Name)
		assert.Equal(t, "bot@example.com", cfg.User.Email)

		t.Cleanup(func() { _ = os.RemoveAll(path) })
	})

	t.Run("clones shallow and without tags", func(t *testing.T) {
		// Both options have no other visible effect and could be dropped silently. Depth keeps
		// the clone cheap; NoTags keeps release tags out of a throwaway clone.
		source, r := initRepoWithCommit(t)

		// Three commits in total, so the clone is distinguishable from both a full clone and a
		// depth-2 one -- go-git still marks a depth-2 clone of a two-commit source as shallow.
		wt, err := r.Worktree()
		require.NoError(t, err)
		var tip plumbing.Hash
		for _, msg := range []string{"second", "third"} {
			tip, err = wt.Commit(msg, &git.CommitOptions{
				AllowEmptyCommits: true,
				Author:            &object.Signature{Name: "t", Email: "t@example.com"},
			})
			require.NoError(t, err)
		}
		_, err = r.CreateTag("v1.0.0", tip, nil)
		require.NoError(t, err)

		head, err := r.Head()
		require.NoError(t, err)

		_, _, path, err := service.CloneRepository(source, head.Name().Short(), "", "Bot", "bot@example.com")
		require.NoError(t, err)
		t.Cleanup(func() { _ = os.RemoveAll(path) })

		shallow, err := service.IsShallowClone(path)
		require.NoError(t, err)
		assert.True(t, shallow, "Depth: 1 should truncate the history")

		cloneShallow, err := git.PlainOpen(path)
		require.NoError(t, err)
		shallowCommits, err := cloneShallow.Storer.Shallow()
		require.NoError(t, err)
		clonedHead, err := cloneShallow.Head()
		require.NoError(t, err)
		// Compared against HEAD, not the graft-point count: there is one either way, so only
		// the tip being the graft point pins the depth to exactly 1.
		require.Len(t, shallowCommits, 1)
		assert.Equal(t, clonedHead.Hash(), shallowCommits[0])

		cloned, err := git.PlainOpen(path)
		require.NoError(t, err)

		tags, err := cloned.Tags()
		require.NoError(t, err)
		tagCount := 0
		require.NoError(t, tags.ForEach(func(*plumbing.Reference) error { tagCount++; return nil }))
		assert.Zero(t, tagCount, "Tags: NoTags should keep the source's tags out of the clone")
	})

	t.Run("returns the clone error for an unreachable repository", func(t *testing.T) {
		_, _, _, err := service.CloneRepository(t.TempDir(), "main", "", "Bot", "bot@example.com")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "git clone")
		require.ErrorIs(t, err, transport.ErrRepositoryNotFound)
	})

	t.Run("returns the clone error for a branch that does not exist", func(t *testing.T) {
		source, _ := initRepoWithCommit(t)

		_, _, _, err := service.CloneRepository(source, "no-such-branch", "", "Bot", "bot@example.com")
		require.Error(t, err)
	})

	t.Run("returns the error when the project directory cannot be created", func(t *testing.T) {
		roService := &GitRepositoryService{logger: zap.NewNop(), fs: afero.NewReadOnlyFs(afero.NewMemMapFs())}

		_, _, _, err := roService.CloneRepository("https://example.com/repo.git", "main", "", "Bot", "bot@example.com")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create project directory")
	})
}

func TestHead(t *testing.T) {
	// Head() is what StartUpdate uses to capture a checkout-mode run's original state before
	// creating drupdater's own throwaway work branch, so it can restore it if the run fails.
	service := NewGitRepositoryService(zap.NewNop())

	t.Run("resolves to the checked-out branch", func(t *testing.T) {
		dir, _ := initRepoWithCommit(t)

		repository, _, _, err := service.OpenRepository(dir, "Bot", "bot@example.com")
		require.NoError(t, err)

		ref, err := repository.Head()
		require.NoError(t, err)
		assert.True(t, ref.Name().IsBranch())
	})

	t.Run("resolves directly to a commit hash when detached", func(t *testing.T) {
		dir, r := initRepoWithCommit(t)
		head, err := r.Head()
		require.NoError(t, err)
		wt, err := r.Worktree()
		require.NoError(t, err)
		require.NoError(t, wt.Checkout(&git.CheckoutOptions{Hash: head.Hash()}))

		repository, _, _, err := service.OpenRepository(dir, "Bot", "bot@example.com")
		require.NoError(t, err)

		ref, err := repository.Head()
		require.NoError(t, err)
		assert.False(t, ref.Name().IsBranch())
		assert.Equal(t, head.Hash(), ref.Hash())
	})
}

func TestIsShallowClone(t *testing.T) {
	service := NewGitRepositoryService(zap.NewNop())

	t.Run("a full checkout is not shallow", func(t *testing.T) {
		dir, _ := initRepoWithCommit(t)

		shallow, err := service.IsShallowClone(dir)
		require.NoError(t, err)
		assert.False(t, shallow)
	})

	t.Run("a depth-limited clone is shallow", func(t *testing.T) {
		source, r := initRepoWithCommit(t)
		wt, err := r.Worktree()
		require.NoError(t, err)
		// A second commit gives the depth-1 clone below ancestry to actually truncate.
		_, err = wt.Commit("second", &git.CommitOptions{
			AllowEmptyCommits: true,
			Author:            &object.Signature{Name: "t", Email: "t@example.com"},
		})
		require.NoError(t, err)
		head, err := r.Head()
		require.NoError(t, err)

		_, _, path, err := service.CloneRepository(source, head.Name().Short(), "", "Bot", "bot@example.com")
		require.NoError(t, err)
		t.Cleanup(func() { _ = os.RemoveAll(path) })

		shallow, err := service.IsShallowClone(path)
		require.NoError(t, err)
		assert.True(t, shallow)
	})

	t.Run("errors on a non-repository path", func(t *testing.T) {
		_, err := service.IsShallowClone(t.TempDir())
		require.Error(t, err)
	})
}

// isolateGitConfig gives these tests a CI runner's identity-less environment, not the
// developer's own ~/.gitconfig.
func isolateGitConfig(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	return home
}

func TestIsSomethingStagedInPathOnStatusError(t *testing.T) {
	core, logs := observer.New(zapcore.ErrorLevel)
	service := NewGitRepositoryService(zap.New(core))

	worktree := NewMockWorktree(t)
	// Non-empty on purpose: with an empty status the guarded and unguarded versions both
	// return false, and the test could not tell them apart.
	worktree.EXPECT().Status().Return(git.Status{
		"file1.txt": &git.FileStatus{Staging: git.Modified},
	}, assert.AnError)

	assert.False(t, service.IsSomethingStagedInPath(worktree, ""))
	assert.Equal(t, 1, logs.FilterMessage("failed to get worktree status").Len(),
		"a failed status must surface in the log rather than pass as 'nothing staged'")
}

func TestPrepareCheckoutKeepsExistingGlobalIdentity(t *testing.T) {
	service := NewGitRepositoryService(zap.NewNop())

	// The fallback fires only when nothing supplies an identity: overwriting a developer's
	// global one would attribute their later commits to drupdater.
	home := isolateGitConfig(t)
	require.NoError(t, os.WriteFile(filepath.Join(home, ".gitconfig"),
		[]byte("[user]\n\tname = Real Person\n\temail = real@example.com\n"), 0o600))

	dir := t.TempDir()
	_, err := git.PlainInit(dir, false)
	require.NoError(t, err)

	_, _, _, err = service.OpenRepository(dir, "", "")
	require.NoError(t, err)

	r, err := git.PlainOpen(dir)
	require.NoError(t, err)
	cfg, err := r.Config()
	require.NoError(t, err)
	assert.NotEqual(t, defaultCommitName, cfg.User.Name)
	assert.NotEqual(t, defaultCommitEmail, cfg.User.Email)
}

func TestPrepareCheckoutKeepsExplicitIdentity(t *testing.T) {
	service := NewGitRepositoryService(zap.NewNop())

	// A caller-supplied identity must win over the fallback, or every commit is attributed to
	// the generic default instead of the bot account the run authenticated as.
	isolateGitConfig(t)

	dir := t.TempDir()
	_, err := git.PlainInit(dir, false)
	require.NoError(t, err)

	_, _, _, err = service.OpenRepository(dir, "Bot", "bot@example.com")
	require.NoError(t, err)

	r, err := git.PlainOpen(dir)
	require.NoError(t, err)
	cfg, err := r.Config()
	require.NoError(t, err)
	assert.Equal(t, "Bot", cfg.User.Name)
	assert.Equal(t, "bot@example.com", cfg.User.Email)
}

func TestPrepareCheckoutReportsHookRemovalFailure(t *testing.T) {
	isolateGitConfig(t)

	dir := t.TempDir()
	_, err := git.PlainInit(dir, false)
	require.NoError(t, err)
	hooks := filepath.Join(dir, ".git", "hooks")
	require.NoError(t, os.MkdirAll(hooks, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(hooks, "prepare-commit-msg"), []byte("#!/bin/sh\n"), 0o600))

	// Stat succeeds but Remove cannot: a hook that is found and then left in place would let
	// `drush config:export --commit` run it, which is exactly what the removal prevents.
	roService := &GitRepositoryService{logger: zap.NewNop(), fs: afero.NewReadOnlyFs(afero.NewOsFs())}

	_, _, _, err = roService.OpenRepository(dir, "Bot", "bot@example.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to remove prepare-commit-msg hook")
}

func TestPrepareCheckoutCommitIdentityFallback(t *testing.T) {
	service := NewGitRepositoryService(zap.NewNop())

	t.Run("falls back when nothing supplies an identity", func(t *testing.T) {
		// A tokenless dry run has no platform to ask and a CI checkout no identity of its own,
		// so without a fallback the first commit dies on "author field is required".
		isolateGitConfig(t)
		dir := t.TempDir()
		_, err := git.PlainInit(dir, false)
		require.NoError(t, err)

		_, _, _, err = service.OpenRepository(dir, "", "")
		require.NoError(t, err)

		r, err := git.PlainOpen(dir)
		require.NoError(t, err)
		cfg, err := r.Config()
		require.NoError(t, err)
		assert.Equal(t, defaultCommitName, cfg.User.Name)
		assert.Equal(t, defaultCommitEmail, cfg.User.Email)
	})

	t.Run("a commit actually succeeds with only the fallback", func(t *testing.T) {
		// The regression this guards is not the config value but the commit itself.
		isolateGitConfig(t)
		dir := t.TempDir()
		_, err := git.PlainInit(dir, false)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hi"), 0o600))

		_, worktree, _, err := service.OpenRepository(dir, "", "")
		require.NoError(t, err)
		_, err = worktree.Add("file.txt")
		require.NoError(t, err)

		hash, err := worktree.Commit("initial", &git.CommitOptions{})
		require.NoError(t, err)
		assert.False(t, hash.IsZero())
	})

	t.Run("a global identity wins over the fallback", func(t *testing.T) {
		// go-git resolves the author from the scoped config, so a developer's global
		// user.name is a perfectly good identity and must not be shadowed by a local default.
		home := isolateGitConfig(t)
		require.NoError(t, os.WriteFile(filepath.Join(home, ".gitconfig"),
			[]byte("[user]\n\tname = Global User\n\temail = global@example.com\n"), 0o600))

		dir := t.TempDir()
		_, err := git.PlainInit(dir, false)
		require.NoError(t, err)

		_, _, _, err = service.OpenRepository(dir, "", "")
		require.NoError(t, err)

		r, err := git.PlainOpen(dir)
		require.NoError(t, err)
		cfg, err := r.Config()
		require.NoError(t, err)
		assert.Empty(t, cfg.User.Name, "no local override should have been written")
		assert.Empty(t, cfg.User.Email)

		scoped, err := r.ConfigScoped(gitConfig.SystemScope)
		require.NoError(t, err)
		assert.Equal(t, "Global User", scoped.User.Name)
	})

	t.Run("an explicit identity wins over both", func(t *testing.T) {
		home := isolateGitConfig(t)
		require.NoError(t, os.WriteFile(filepath.Join(home, ".gitconfig"),
			[]byte("[user]\n\tname = Global User\n\temail = global@example.com\n"), 0o600))

		dir := t.TempDir()
		_, err := git.PlainInit(dir, false)
		require.NoError(t, err)

		_, _, _, err = service.OpenRepository(dir, "Bot", "bot@example.com")
		require.NoError(t, err)

		r, err := git.PlainOpen(dir)
		require.NoError(t, err)
		cfg, err := r.Config()
		require.NoError(t, err)
		assert.Equal(t, "Bot", cfg.User.Name)
		assert.Equal(t, "bot@example.com", cfg.User.Email)
	})
}
