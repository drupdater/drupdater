package composer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/afero"
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

type patchTestConfig struct {
	Patches map[string]map[string]string `json:"patches"`
}

// CheckIfPatchApplies reports whether one patch still applies to packageName at packageVersion.
func (s *CLI) CheckIfPatchApplies(ctx context.Context, projectDir string, packageName string, packageVersion string, patchPath string) (bool, error) {
	return s.CheckIfPatchesApply(ctx, projectDir, packageName, packageVersion, []string{patchPath})
}

// requireIntoScratch installs the package under test: true when it installed, false when the
// patch was rejected, error when composer never got the package. The third case must stay
// distinct — false pins the package, which is wrong for an unreachable registry. Not --quiet:
// that suppresses the message this classification reads.
func (s *CLI) requireIntoScratch(ctx context.Context, dir string, packageName string, packageVersion string) (bool, error) {
	out, err := s.execComposer(ctx, dir,
		"require", "--ignore-platform-reqs", packageName+":"+constraint(packageVersion), "--with-all-dependencies")
	if err == nil {
		return true, nil
	}

	if reason, unresolvable := unresolvableReason(out); unresolvable {
		return false, fmt.Errorf("could not obtain %s %s to test its patches (%s): %w", packageName, packageVersion, reason, err)
	}

	return false, nil //nolint:nilerr // composer obtained the package and the patch was rejected
}

// unresolvableReason reports whether composer could not obtain the package, and why. A rejected
// patch wins outright: the dist-to-source fallback logs "could not be downloaded (404)" and then
// succeeds, so the unresolvable patterns alone would misread a genuine rejection.
//
// Both match composer's English output, so prefer adding a pattern to loosening one.
func unresolvableReason(out string) (string, bool) {
	lower := strings.ToLower(out)

	// The two wordings composer-patches v1 uses for a patch failure.
	if strings.Contains(lower, "could not apply patch") || strings.Contains(lower, "cannot apply patch") {
		return "", false
	}

	for _, candidate := range unresolvablePatterns {
		if strings.Contains(lower, candidate.pattern) {
			return candidate.reason, true
		}
	}
	return "", false
}

var unresolvablePatterns = []struct{ pattern, reason string }{
	{"could not find package", "not available from any configured repository"},
	{"could not find a matching version", "not available from any configured repository"},
	{"could not be found", "not available from any configured repository"},
	{"invalid credentials", "repository authentication failed"},
	{"authentication required", "repository authentication failed"},
	{"could not be downloaded", "a required download failed"},
}

// CheckIfPatchesApply reports whether patchPaths all apply together. The keys are zero-padded
// indexes because composer-patches v1 applies patches in key order.
func (s *CLI) CheckIfPatchesApply(ctx context.Context, projectDir string, packageName string, packageVersion string, patchPaths []string) (bool, error) {
	dir, err := s.scratch.get(s.fs)
	if err != nil {
		return false, err
	}
	if err := s.resetScratchProject(dir, projectDir); err != nil {
		return false, err
	}

	patchMap := make(map[string]string, len(patchPaths))
	for i, p := range patchPaths {
		patchMap[fmt.Sprintf("%010d", i)] = p
	}

	// Marshalled rather than formatted, so special characters in the arguments stay escaped.
	patchesJSONBytes, err := json.Marshal(patchTestConfig{
		Patches: map[string]map[string]string{packageName: patchMap},
	})
	if err != nil {
		return false, fmt.Errorf("failed to marshal patch config: %w", err)
	}

	if err := afero.WriteFile(s.fs, dir+"/composer.patches.json", patchesJSONBytes, 0644); err != nil {
		return false, err
	}

	return s.requireIntoScratch(ctx, dir, packageName, packageVersion)
}
