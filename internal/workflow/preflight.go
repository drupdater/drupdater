package workflow

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/spf13/afero"

	"github.com/drupdater/drupdater/internal/composer"
)

// CheckResult is one preflight outcome, from "drupdater check" or from a run's own guard.
type CheckResult struct {
	Name string
	OK   bool
	// Detail explains a failure; empty on a plain pass.
	Detail string
}

// CheckOK reports a passing check.
func CheckOK(name string) CheckResult {
	return CheckResult{Name: name, OK: true}
}

// CheckFailed reports a failing check, with detail explaining why.
func CheckFailed(name string, detail string) CheckResult {
	return CheckResult{Name: name, OK: false, Detail: detail}
}

// ShallowCloneChecker is all CheckGitHistoryComplete needs, so a caller can supply a small double.
type ShallowCloneChecker interface {
	IsShallowClone(ctx context.Context, path string) (bool, error)
}

// CheckGitHistoryComplete reports whether workingDir is a shallow clone, which commits fine but
// fails the push much later with a cryptic "object not found".
func CheckGitHistoryComplete(ctx context.Context, repository ShallowCloneChecker, workingDir string) CheckResult {
	const name = "git history complete (not a shallow clone)"

	shallow, err := repository.IsShallowClone(ctx, workingDir)
	if err != nil {
		return CheckFailed(name, fmt.Sprintf("could not determine clone depth: %s", err))
	}
	if shallow {
		return CheckFailed(name, `shallow checkout detected: fetch full history (set "fetch-depth: 0" in GitHub Actions, or GIT_DEPTH: "0" in GitLab CI)`)
	}
	return CheckOK(name)
}

// PlatformReqsChecker is narrow for the same reason as ShallowCloneChecker.
type PlatformReqsChecker interface {
	CheckPlatformReqs(ctx context.Context, dir string) (string, error)
}

// CheckPlatformRequirements reports whether the runtime PHP satisfies composer.lock's platform.
func CheckPlatformRequirements(ctx context.Context, composer PlatformReqsChecker, workingDir string) CheckResult {
	const name = "PHP platform requirements satisfied"

	out, err := composer.CheckPlatformReqs(ctx, workingDir)
	if err != nil {
		return CheckFailed(name, out)
	}
	return CheckOK(name)
}

// AgentChecker is narrow for the same reason as ShallowCloneChecker.
type AgentChecker interface {
	CheckAgent(ctx context.Context, dir string) error
}

// CheckComposerAgent reports whether composer has drupdater's agent, without which no patch can
// be tested and composer_patches would leave every patched package unpinned.
func CheckComposerAgent(ctx context.Context, composer AgentChecker, workingDir string) CheckResult {
	const name = "drupdater agent installed in Composer"

	if err := composer.CheckAgent(ctx, workingDir); err != nil {
		return CheckFailed(name, fmt.Sprintf("%s (run drupdater from its Docker image, which installs the agent)", err))
	}
	return CheckOK(name)
}

// ProjectReader is narrow for the same reason as ShallowCloneChecker.
type ProjectReader interface {
	Project(ctx context.Context, dir string) (composer.Project, error)
}

// CheckSiteSettings looks for the site's settings.php: without it the site was never installed
// here, and every downstream phase has nothing to build on.
func CheckSiteSettings(ctx context.Context, projects ProjectReader, fs afero.Fs, workingDir string, site string) CheckResult {
	name := fmt.Sprintf("site %q: settings.php", site)

	project, err := projects.Project(ctx, workingDir)
	if err != nil {
		return CheckFailed(name, fmt.Sprintf("could not determine web root: %s", err))
	}
	webroot, err := project.RequireWebRoot()
	if err != nil {
		return CheckFailed(name, fmt.Sprintf("could not determine web root: %s", err))
	}

	relPath := filepath.Join(webroot, "sites", site, "settings.php")
	if _, err := fs.Stat(filepath.Join(workingDir, relPath)); err != nil {
		return CheckFailed(name, fmt.Sprintf("not found at %s", relPath))
	}
	return CheckOK(name)
}
