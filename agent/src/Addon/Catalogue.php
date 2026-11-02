<?php

declare(strict_types=1);

namespace Drupdater\Agent\Addon;

/**
 * Every addon drupdater can run. Its order is the order hooks sharing an event and a stage run
 * in: the mandatory addons in this order, then the configured ones in the order configured.
 */
final class Catalogue
{
    /**
     * @param list<CatalogueEntry> $entries
     */
    public function __construct(private readonly array $entries)
    {
        $names = array_map(static fn (CatalogueEntry $entry): string => $entry->name(), $entries);
        if (count(array_unique($names)) !== count($names)) {
            throw new \LogicException('two addons share a name');
        }
    }

    public static function builtin(): self
    {
        return new self([
            // Before composer_patches: its dry run of the update must be free to install plugins.
            new CatalogueEntry(ComposerAllowPlugins::class, mandatory: true),
            new CatalogueEntry(ComposerPatches::class, mandatory: true),
            new CatalogueEntry(ComposerDiff::class, mandatory: true),
            new CatalogueEntry(UpdateHooks::class, mandatory: true),
            // The description joins composer_audit's and unsupported_modules' findings into one
            // end-of-life list, so both always run.
            new CatalogueEntry(ComposerAudit::class, mandatory: true),
            new CatalogueEntry(UnsupportedModules::class, mandatory: true),
            new CatalogueEntry(CodeBeautifier::class, enabledByDefault: true),
            new CatalogueEntry(DeprecationsRemover::class, enabledByDefault: true),
            new CatalogueEntry(TranslationsUpdater::class, enabledByDefault: true),
            new CatalogueEntry(ComposerNormalizer::class, enabledByDefault: true),
        ]);
    }

    /**
     * @return list<CatalogueEntry>
     */
    public function entries(): array
    {
        return $this->entries;
    }

    public function find(string $name): ?CatalogueEntry
    {
        foreach ($this->entries as $entry) {
            if ($entry->name() === $name) {
                return $entry;
            }
        }
        return null;
    }
}
