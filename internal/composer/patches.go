package composer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/afero"
	"go.uber.org/zap"

	"github.com/drupdater/drupdater/internal/agent"
)

// GetDependencyPatches returns targetPackage -> patch files declared by installed dependencies,
// which apply on top of the root composer.json patches.
func (s *CLI) GetDependencyPatches(_ context.Context, dir string) (map[string]map[string]bool, error) {
	content, err := afero.ReadFile(s.fs, dir+"/composer.lock")
	if err != nil {
		return nil, fmt.Errorf("failed to read composer.lock: %w", err)
	}

	var lock struct {
		Packages    []lockPackage `json:"packages"`
		PackagesDev []lockPackage `json:"packages-dev"`
	}
	if err := json.Unmarshal(content, &lock); err != nil {
		return nil, fmt.Errorf("failed to unmarshal composer.lock: %w", err)
	}

	result := make(map[string]map[string]bool)
	for _, pkg := range append(lock.Packages, lock.PackagesDev...) {
		// extra is optional and may be serialized as [] when empty; tolerate both.
		var extra struct {
			Patches map[string]map[string]string `json:"patches"`
		}
		if len(pkg.Extra) == 0 || json.Unmarshal(pkg.Extra, &extra) != nil {
			continue
		}
		for targetPackage, byDescription := range extra.Patches {
			for _, file := range byDescription {
				if result[targetPackage] == nil {
					result[targetPackage] = make(map[string]bool)
				}
				result[targetPackage][file] = true
			}
		}
	}
	return result, nil
}

type lockPackage struct {
	Extra json.RawMessage `json:"extra"`
}

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
