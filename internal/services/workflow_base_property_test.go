package services

import (
	"testing"

	"github.com/drupdater/drupdater/internal/report"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/stretchr/testify/assert"
	"pgregory.net/rapid"
)

// report.PackageChange mirrors composer.PackageChange so a refactor cannot rename a published
// field. That only pays off if drift is noticed, and a hand-written example covers only the
// fields its author filled in — generating the whole struct is what makes a dropped field fail.

func packageChangeGen() *rapid.Generator[composer.PackageChange] {
	return rapid.Custom(func(t *rapid.T) composer.PackageChange {
		return composer.PackageChange{
			Action:  rapid.SampledFrom([]string{"Install", "Upgrade", "Downgrade", "Remove"}).Draw(t, "action"),
			Package: rapid.StringMatching(`[a-z0-9_-]{1,10}/[a-z0-9_-]{1,10}`).Draw(t, "package"),
			From:    rapid.StringMatching(`([0-9]{1,2}\.[0-9]{1,2}\.[0-9]{1,2})?`).Draw(t, "from"),
			To:      rapid.StringMatching(`([0-9]{1,2}\.[0-9]{1,2}\.[0-9]{1,2})?`).Draw(t, "to"),
		}
	})
}

func TestPropertyToReportPackagesCarriesEveryField(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		changes := rapid.SliceOfN(packageChangeGen(), 1, 8).Draw(t, "changes")

		out := toReportPackages(changes)

		assert.Len(t, out, len(changes))
		for i, change := range changes {
			assert.Equal(t, report.PackageChange{
				Action:  change.Action,
				Package: change.Package,
				From:    change.From,
				To:      change.To,
			}, out[i], "package change %d", i)
		}
	})
}

func TestPropertyToReportPackagesReturnsNilForNothing(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		empty := rapid.SampledFrom([][]composer.PackageChange{nil, {}}).Draw(t, "empty")

		// Nil rather than an empty slice: the report's field is `omitempty`, so an empty slice
		// would publish "packages": [] where the schema says the key is absent.
		assert.Nil(t, toReportPackages(empty))
	})
}
