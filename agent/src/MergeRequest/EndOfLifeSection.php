<?php

declare(strict_types=1);

namespace Drupdater\Agent\MergeRequest;

/**
 * unsupported_modules and composer_audit's abandoned packages, joined: one finding to a reviewer,
 * so one table, sorted by name so the two sources interleave rather than read as two lists.
 */
final class EndOfLifeSection implements Section
{
    public function name(): string
    {
        return 'unsupported_modules';
    }

    public function render(\stdClass $addons, bool $links): ?string
    {
        $rows = [];
        foreach (Read::objects($addons->unsupported_modules ?? null, 'unsupported_modules', 'the section') as $module) {
            $recommended = Read::string($module, 'recommended_version', 'unsupported_modules');
            // drush reports "None" when there is no supported release — a status, not a version.
            $rows[] = [
                Read::string($module, 'name', 'unsupported_modules'),
                'Unsupported module',
                Read::string($module, 'installed_version', 'unsupported_modules'),
                $recommended === '' || $recommended === 'None' ? 'No supported release — replace it' : 'Update to ' . $recommended,
            ];
        }
        $audit = Read::object($addons->composer_audit ?? null, 'composer_audit', 'the section');
        foreach (Read::objects($audit->abandoned ?? null, 'composer_audit', 'abandoned') as $package) {
            $replacement = Read::string($package, 'replacement', 'composer_audit');
            $rows[] = [
                Read::string($package, 'packageName', 'composer_audit'),
                'Abandoned package',
                '',
                $replacement === '' ? 'No replacement suggested' : 'Replace with ' . $replacement,
            ];
        }
        if ($rows === []) {
            return null;
        }
        usort($rows, static fn (array $a, array $b): int => strcmp($a[0], $b[0]));

        $out = "## ⚠️ Unsupported modules and abandoned packages\n\n"
            . 'The following are no longer maintained: an unsupported module has no supported release on Drupal.org, '
            . 'an abandoned package has been marked as such by its maintainers. Neither will receive further fixes, '
            . 'including security ones, and no update will resolve it. Each needs a decision — replace it, adopt it, '
            . "or accept the risk.\n\n"
            . "| Name | Status | Installed version | Recommendation |\n"
            . "| ---- | ------ | ----------------- | -------------- |\n";
        foreach ($rows as [$name, $status, $installed, $recommendation]) {
            $out .= sprintf(
                "| %s | %s | %s | %s |\n",
                Markdown::cell($name),
                $status,
                $installed === '' ? '—' : Markdown::cell($installed),
                Markdown::cell($recommendation),
            );
        }
        return $out;
    }
}
