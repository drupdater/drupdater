package composer

import (
	"context"
	"fmt"
	"strings"

	"go.uber.org/zap"

	"github.com/drupdater/drupdater/internal/agent"
)

// CheckAgent confirms the drupdater agent is installed in composer and speaks this binary's protocol.
func (s *CLI) CheckAgent(ctx context.Context, dir string) error {
	_, err := agent.Call[struct{}](ctx, s.command(dir), "drupdater:handshake", struct{}{})
	return err
}

// CheckIfPatchApplies reports whether one patch still applies to packageName at packageVersion.
func (s *CLI) CheckIfPatchApplies(ctx context.Context, projectDir string, packageName string, packageVersion string, patchPath string) (bool, error) {
	return s.CheckIfPatchesApply(ctx, projectDir, packageName, packageVersion, []string{patchPath})
}

type patchCheckRequest struct {
	Package   string   `json:"package"`
	Version   string   `json:"version"`
	Reference string   `json:"reference"`
	Patches   []string `json:"patches"`
}

type patchCheckResult struct {
	Outcome string `json:"outcome"`
	Patch   string `json:"patch"`
}

// CheckIfPatchesApply reports whether patchPaths apply in order to packageName at packageVersion,
// as the agent finds it through the project's own repositories. The error is an *agent.Error
// when the package or a patch could not be fetched: the patches were then never tested.
func (s *CLI) CheckIfPatchesApply(ctx context.Context, projectDir string, packageName string, packageVersion string, patchPaths []string) (bool, error) {
	version, reference, _ := strings.Cut(packageVersion, " ")
	result, err := agent.Call[patchCheckResult](ctx, s.command(projectDir), "drupdater:patch-check", patchCheckRequest{
		Package:   packageName,
		Version:   version,
		Reference: reference,
		Patches:   patchPaths,
	})
	if err != nil {
		return false, fmt.Errorf("could not check the patches of %s %s: %w", packageName, packageVersion, err)
	}
	switch result.Outcome {
	case "applies":
		return true, nil
	case "rejected":
		s.logger.Debug("patch rejected", zap.String("package", packageName), zap.String("patch", result.Patch))
		return false, nil
	}
	return false, fmt.Errorf("drupdater:patch-check answered an unknown outcome %q", result.Outcome)
}
