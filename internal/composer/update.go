package composer

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/afero"
	"go.uber.org/zap"
	"golang.org/x/mod/semver"
)

type PackageChange struct {
	Action  string // Install, Upgrade, Remove, Downgrade
	Package string
	From    string
	To      string
}

// Update resolves into composer.json and composer.lock only; Install brings vendor/ to the result.
// A dry run leaves both files as it found them.
//
// The changes are read from a diff of composer.lock before and after, never from the console
// output, whose wording composer does not keep stable and which drops dev-branch commits.
func (s *CLI) Update(ctx context.Context, dir string, packages []string, packagesToKeep []string, minimalChanges bool, dryRun bool) (changes []PackageChange, err error) {
	lockPath := filepath.Join(dir, "composer.lock")
	baseLock, err := afero.ReadFile(s.fs, lockPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read composer.lock: %w", err)
	}

	// Lock only: vendor/ keeps the old code until Install, so sites can still be installed at it
	// after the update is known to change anything.
	args := append([]string{"update", "--no-interaction", "--no-progress", "--optimize-autoloader", "--with-all-dependencies", "--no-ansi", "--ignore-platform-reqs", "--no-install"}, packages...)
	for _, packageToKeep := range packagesToKeep {
		name, version, _ := strings.Cut(packageToKeep, ":")
		args = append(args, fmt.Sprintf("--with=%s:%s", name, constraint(version)))
	}
	if minimalChanges {
		args = append(args, "--minimal-changes")
	}
	if dryRun {
		// composer's own --dry-run writes no lock, leaving nothing to diff, so the files are put
		// back instead. No scripts: the project never asked for them on a result that is undone.
		args = append(args, "--no-scripts")
		restore, snapshotErr := s.snapshot(dir, "composer.json", "composer.lock")
		if snapshotErr != nil {
			return nil, snapshotErr
		}
		defer func() { err = errors.Join(err, restore()) }()
	} else {
		args = append(args, "--bump-after-update")
	}

	if out, err := s.execComposer(ctx, dir, args...); err != nil {
		return nil, fmt.Errorf("failed to update dependencies: %w, output: %s, arg: %v", err, out, args)
	}

	base, err := afero.TempFile(s.fs, "", "drupdater-base-*.lock")
	if err != nil {
		return nil, fmt.Errorf("failed to create the base lock file: %w", err)
	}
	defer func() {
		if err := s.fs.Remove(base.Name()); err != nil {
			s.logger.Warn("failed to remove the base lock file", zap.String("path", base.Name()), zap.Error(err))
		}
	}()
	_, writeErr := base.Write(baseLock)
	if err := errors.Join(writeErr, base.Close()); err != nil {
		return nil, fmt.Errorf("failed to write the base lock file: %w", err)
	}

	entries, err := s.diff(ctx, dir, "--base="+base.Name(), "--target=composer.lock")
	if err != nil {
		return nil, fmt.Errorf("failed to diff composer.lock: %w", err)
	}
	return packageChanges(entries)
}

// snapshot reads files under dir now and returns a func that writes them back as they were.
func (s *CLI) snapshot(dir string, files ...string) (func() error, error) {
	contents := make(map[string][]byte, len(files))
	for _, file := range files {
		path := filepath.Join(dir, file)
		content, err := afero.ReadFile(s.fs, path)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", file, err)
		}
		contents[path] = content
	}
	return func() error {
		var errs []error
		for path, content := range contents {
			// Equivalent mutant: the file was just read, so it exists and the mode is never used.
			// mutator-disable-next-line numbers/incrementer,numbers/decrementer
			if err := afero.WriteFile(s.fs, path, content, 0644); err != nil {
				errs = append(errs, fmt.Errorf("failed to restore %s: %w", filepath.Base(path), err))
			}
		}
		return errors.Join(errs...)
	}, nil
}

// packageAction maps a composer-diff operation to a PackageChange action. A "change" is a new
// commit of the same version, which composer itself reports as an upgrade.
func packageAction(operation string) (string, bool) {
	switch operation {
	case "upgrade", "change":
		return "Upgrade", true
	case "downgrade":
		return "Downgrade", true
	case "remove":
		return "Remove", true
	case "install":
		return "Install", true
	}
	return "", false
}

// actionOrder is the order the changes come back in, which both the report and the MR carry.
var actionOrder = []string{"Upgrade", "Downgrade", "Remove", "Install"}

// packageChanges sorts by action, then package, so the result does not depend on how
// composer-diff split production from dev packages.
func packageChanges(entries []DiffEntry) ([]PackageChange, error) {
	var changes []PackageChange
	for _, entry := range entries {
		action, ok := packageAction(entry.Operation)
		if !ok {
			return nil, fmt.Errorf("unknown composer-diff operation %q for %s", entry.Operation, entry.Package)
		}
		changes = append(changes, PackageChange{Action: action, Package: entry.Package, From: entry.From, To: entry.To})
	}
	changes = joinMoves(changes)
	slices.SortFunc(changes, func(a, b PackageChange) int {
		return cmp.Or(
			cmp.Compare(slices.Index(actionOrder, a.Action), slices.Index(actionOrder, b.Action)),
			strings.Compare(a.Package, b.Package),
		)
	})
	return changes, nil
}

// joinMoves turns the remove and install composer-diff reports for a package that moved between
// require and require-dev into the one change the lock made, or none if its version stayed.
func joinMoves(changes []PackageChange) []PackageChange {
	removed := map[string]string{}
	installed := map[string]string{}
	for _, c := range changes {
		switch c.Action {
		case "Remove":
			removed[c.Package] = c.From
		case "Install":
			installed[c.Package] = c.To
		}
	}

	var joined []PackageChange
	for _, c := range changes {
		from, wasRemoved := removed[c.Package]
		to, wasInstalled := installed[c.Package]
		if !wasRemoved || !wasInstalled {
			joined = append(joined, c)
			continue
		}
		if c.Action == "Remove" && from != to {
			joined = append(joined, PackageChange{Action: direction(from, to), Package: c.Package, From: from, To: to})
		}
	}
	return joined
}

// direction compares as semver where both versions are one; like composer-diff's "change", a
// dev branch has no order and counts as an upgrade.
func direction(from string, to string) string {
	v, w := "v"+strings.TrimPrefix(from, "v"), "v"+strings.TrimPrefix(to, "v")
	if semver.IsValid(v) && semver.IsValid(w) && semver.Compare(v, w) > 0 {
		return "Downgrade"
	}
	return "Upgrade"
}

// constraint turns a version as composer prints it into one composer accepts back: a dev
// version prints as "1.x-dev 0424dff", but a constraint names its commit as "1.x-dev#0424dff".
func constraint(version string) string {
	if name, ref, ok := strings.Cut(version, " "); ok {
		return name + "#" + ref
	}
	return version
}
