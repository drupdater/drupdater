<?php

declare(strict_types=1);

namespace Drupdater\Agent\MergeRequest;

/** The advisories only: composer_audit's abandoned packages belong to the end-of-life table. */
final class AuditSection implements Section
{
    private const NAME = 'composer_audit';

    public function name(): string
    {
        return self::NAME;
    }

    public function render(\stdClass $addons, bool $links): ?string
    {
        $section = Read::object($addons->composer_audit ?? null, self::NAME, 'the section');
        $fixed = Read::objects($section->fixed ?? null, self::NAME, 'fixed');
        $remaining = Read::objects($section->remaining ?? null, self::NAME, 'remaining');
        if ($fixed === [] && $remaining === []) {
            return null;
        }

        $summary = $remaining === []
            ? 'All security issues have been resolved.'
            : sprintf('There are still %d unresolved issue%s. Please investigate manually.', count($remaining), count($remaining) > 1 ? 's' : '');
        $out = "## 🛡️ Security Report\n\nThe security report shows fixed and unfixed security vulnerabilities. {$summary}\n\n"
            . "| Status | CVE      | Title | Severity | Package  |\n"
            . "| ------ | -------- | ----- | -------- | -------- |\n";
        foreach ([['✅', $fixed], ['⛔', $remaining]] as [$status, $advisories]) {
            foreach ($advisories as $advisory) {
                $out .= sprintf(
                    "| %s     | %s | %s | %s | %s |\n",
                    $status,
                    self::cell($advisory, 'cve'),
                    self::cell($advisory, 'title'),
                    self::cell($advisory, 'severity'),
                    self::cell($advisory, 'packageName'),
                );
            }
        }
        return $out;
    }

    private static function cell(\stdClass $advisory, string $key): string
    {
        return Markdown::cell(Read::string($advisory, $key, self::NAME));
    }
}
