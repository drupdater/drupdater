package composer

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/drupdater/drupdater/internal/php"
)

// DiffEntry is one package that differs between the committed composer.lock and the working one.
type DiffEntry struct {
	Package string
	// Operation is composer-diff's: install, upgrade, downgrade, remove or change, the last a new
	// commit of the same version.
	Operation string
	// From is empty for an install, To for a removal.
	From string
	To   string
	// Dev marks a packages-dev entry; Direct, one composer.json requires by name.
	Dev    bool
	Direct bool
	// CompareURL and ProjectURL are empty where composer-diff cannot derive one from the source.
	CompareURL string
	ProjectURL string
}

// Diff lists what changed in composer.lock since HEAD, production packages first, each by name.
func (s *CLI) Diff(ctx context.Context, dir string) ([]DiffEntry, error) {
	return s.diff(ctx, dir, "--with-links")
}

func (s *CLI) diff(ctx context.Context, dir string, args ...string) ([]DiffEntry, error) {
	raw, err := php.JSON[struct {
		Packages    json.RawMessage `json:"packages"`
		PackagesDev json.RawMessage `json:"packages-dev"`
	}](ctx, s.command(dir), append([]string{"diff", "--format=json"}, args...)...)
	if err != nil {
		return nil, fmt.Errorf("failed to parse composer diff output: %w", err)
	}

	prod, err := decodeDiffEntries(raw.Packages, false)
	if err != nil {
		return nil, err
	}
	dev, err := decodeDiffEntries(raw.PackagesDev, true)
	if err != nil {
		return nil, err
	}

	return append(prod, dev...), nil
}

// decodeDiffEntries accepts both shapes composer-diff emits: PHP encodes an empty list as [].
func decodeDiffEntries(data json.RawMessage, dev bool) ([]DiffEntry, error) {
	if len(data) == 0 || string(data) == "[]" {
		return nil, nil
	}

	var rows map[string]struct {
		Name          string `json:"name"`
		Direct        bool   `json:"direct"`
		Operation     string `json:"operation"`
		VersionBase   string `json:"version_base"`
		VersionTarget string `json:"version_target"`
		Compare       string `json:"compare"`
		Link          string `json:"link"`
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, fmt.Errorf("failed to parse composer diff output: %w", err)
	}

	entries := make([]DiffEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, DiffEntry{
			Package:    row.Name,
			Operation:  row.Operation,
			From:       row.VersionBase,
			To:         row.VersionTarget,
			Dev:        dev,
			Direct:     row.Direct,
			CompareURL: row.Compare,
			ProjectURL: row.Link,
		})
	}
	slices.SortFunc(entries, func(a, b DiffEntry) int { return strings.Compare(a.Package, b.Package) })

	return entries, nil
}
