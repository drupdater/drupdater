package services

import (
	"crypto/md5"
	"fmt"
	"math/rand"
	"strings"

	"github.com/drupdater/drupdater/internal"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"go.uber.org/zap"
)

type RepositoryService interface {
	CloneRepository(repository string, branch string, token string) (internal.Repository, internal.Worktree, string, error)
	GetHeadCommit(repository internal.Repository) (*object.Commit, error)
	IsSomethingStagedInPath(worktree internal.Worktree, dir string) bool
	BranchExists(repository internal.Repository, branch string) (bool, error)
}

type GitRepositoryService struct {
	logger *zap.Logger
}

func NewGitRepositoryService(logger *zap.Logger) *GitRepositoryService {
	return &GitRepositoryService{
		logger: logger,
	}
}

func (rs *GitRepositoryService) CloneRepository(repository string, branch string, token string) (internal.Repository, internal.Worktree, string, error) {

	randString := func(n int) string {
		const letters = "abcdef0123456789"
		b := make([]byte, n)
		for i := range b {
			b[i] = letters[rand.Intn(len(letters))]
		}
		return string(b)
	}

	random := randString(6)
	hash := md5.Sum([]byte(repository))
	tmpDirName := fmt.Sprintf("/tmp/%x/checkout-%s", hash, random)

	checkout, err := git.PlainClone(tmpDirName, false, &git.CloneOptions{
		URL:           repository,
		Depth:         1,
		ReferenceName: plumbing.NewBranchReferenceName(branch),
		Auth: &http.BasicAuth{
			Username: "du", // yes, this can be anything except an empty string
			Password: token,
		},
		Tags: git.NoTags,
	})

	if err != nil {
		return nil, nil, "", err
	}

	// Set the user name and email for the commit
	config, _ := checkout.Config()
	config.User.Name = "DrupalUpdaterBot"
	config.User.Email = "technology@drupdater.com"
	err = checkout.SetConfig(config)
	if err != nil {
		return checkout, nil, "", err
	}

	w, err := checkout.Worktree()
	if err != nil {
		return checkout, nil, "", err
	}

	// @TODO: Verify if this is necessary
	// Remove prepare-commit-msg hook because it does not work with the --no-verify option.
	if _, err := w.Filesystem.Stat(".git/hooks/prepare-commit-msg"); err == nil {
		err = w.Filesystem.Remove(".git/hooks/prepare-commit-msg")
		if err != nil {
			rs.logger.Error("failed to remove prepare-commit-msg hook", zap.Error(err))
			return checkout, w, "", err
		}
	}

	return checkout, w, w.Filesystem.Root(), nil
}

func (rs *GitRepositoryService) BranchExists(repository internal.Repository, branch string) (bool, error) {
	// Get list of remote branches
	remoteRefs, err := repository.References()
	if err != nil {
		return false, err
	}

	// Iterate through the references and check if branch exists
	remoteBranchRef := fmt.Sprintf("refs/remotes/origin/%s", branch)

	for {
		ref, err := remoteRefs.Next()
		if err != nil {
			break // End of refs
		}

		rs.logger.Debug("checking branch", zap.String("branch", ref.Name().String()))
		if ref.Name().String() == remoteBranchRef {
			return true, nil
		}
	}
	return false, nil
}

func (rs *GitRepositoryService) GetHeadCommit(repository internal.Repository) (*object.Commit, error) {
	head, _ := repository.Head()
	object, err := repository.CommitObject(head.Hash())
	if err != nil {
		return object, err
	}

	return object, nil
}

func (rs *GitRepositoryService) IsSomethingStagedInPath(worktree internal.Worktree, dir string) bool {
	status, err := worktree.Status()
	if err != nil {
		rs.logger.Error("failed to get worktree status", zap.Error(err))
		return false
	}

	for filePath, s := range status {
		rs.logger.Debug("checking file", zap.String("file", filePath), zap.Any("status", s.Staging))

		if s.Staging != git.Unmodified && strings.Contains(filePath, dir) {
			rs.logger.Debug("file staged", zap.String("file", filePath))
			return true
		}
	}

	return false
}
