<?php

declare(strict_types=1);

namespace Drupdater\Agent\MergeRequest;

/** Rendered whenever composer_patches reports, even with nothing under it. */
final class PatchesSection implements Section
{
    private const NAME = 'composer_patches';

    public function name(): string
    {
        return self::NAME;
    }

    public function render(\stdClass $addons, bool $links): ?string
    {
        $section = Read::object($addons->composer_patches ?? null, self::NAME, 'the section');
        if ($section === null) {
            return null;
        }
        $out = "## 🩹 Patch updates\n\n";

        $removed = Read::objects($section->removed ?? null, self::NAME, 'removed');
        if ($removed !== []) {
            $out .= "✅ **Removed Patches**\n\nThe following patches were removed as they are no longer needed:\n\n";
            foreach ($removed as $patch) {
                $out .= sprintf(
                    "- %s:\n\n  - Package: %s\n  - Patch: `%s`\n  - Reason: %s\n\n",
                    self::string($patch, 'patch_description'),
                    self::string($patch, 'package'),
                    self::string($patch, 'patch_path'),
                    self::string($patch, 'reason'),
                );
            }
        }

        $updated = Read::objects($section->updated ?? null, self::NAME, 'updated');
        if ($updated !== []) {
            $out .= "🔄 **Updated Patches**\n\n";
            foreach ($updated as $patch) {
                $out .= sprintf(
                    "- %s\n\n  - Package: %s\n  - Previous patch: `%s`\n  - New patch: `%s`\n\n",
                    self::string($patch, 'patch_description'),
                    self::string($patch, 'package'),
                    self::string($patch, 'previous_patch_path'),
                    self::string($patch, 'new_patch_path'),
                );
            }
        }

        $conflicts = Read::objects($section->conflicts ?? null, self::NAME, 'conflicts');
        if ($conflicts !== []) {
            $out .= "⚠️ **Packages Not Updated Due to Patch Conflicts**\n\n";
            foreach ($conflicts as $conflict) {
                $out .= sprintf(
                    "- %s was kept at version %s because\n\n  - Description: %s\n  - Patch: `%s`\n"
                    . "  - Reason: Patch is not compatible with the new version %s and a newer patch that is compatible with the new version is not available\n\n",
                    self::string($conflict, 'package'),
                    self::string($conflict, 'fixed_version'),
                    self::string($conflict, 'patch_description'),
                    self::string($conflict, 'patch_path'),
                    self::string($conflict, 'new_version'),
                );
            }
        }
        return $out;
    }

    private static function string(\stdClass $object, string $key): string
    {
        return Read::string($object, $key, self::NAME);
    }
}
