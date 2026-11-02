<?php

declare(strict_types=1);

namespace Drupdater\Agent\MergeRequest;

/** composer_diff's changes, prod and dev in two tables. Only this section has links to drop. */
final class DependencySection implements Section
{
    private const NAME = 'composer_diff';

    public function name(): string
    {
        return self::NAME;
    }

    public function render(\stdClass $addons, bool $links): ?string
    {
        $tables = ['Prod Packages' => [], 'Dev Packages' => []];
        foreach (Read::objects($addons->composer_diff ?? null, self::NAME, 'the section') as $change) {
            $tables[Read::bool($change, 'dev', self::NAME) ? 'Dev Packages' : 'Prod Packages'][] = $change;
        }
        $tables = array_filter($tables);
        if ($tables === []) {
            return null;
        }

        $out = "## 🛠️ Dependency updates\n\n<details open>\n<summary>Open/close</summary>\n";
        foreach ($tables as $title => $changes) {
            $out .= "\n| {$title} | Operation | Base | Target |" . ($links ? ' Link |' : '') . "\n";
            $out .= '| --- | --- | --- | --- |' . ($links ? ' --- |' : '') . "\n";
            foreach ($changes as $change) {
                $out .= self::row($change, $links);
            }
        }
        return $out . "\n</details>\n";
    }

    private static function row(\stdClass $change, bool $links): string
    {
        $package = Markdown::cell(self::string($change, 'package'));
        $project = self::string($change, 'project_url');
        if ($links && $project !== '') {
            $package = "[{$package}]({$project})";
        }
        $row = sprintf(
            '| %s | %s | %s | %s |',
            $package,
            Markdown::operation(self::string($change, 'operation')),
            Markdown::version(self::string($change, 'from')),
            Markdown::version(self::string($change, 'to')),
        );
        if ($links) {
            $compare = self::string($change, 'compare_url');
            $row .= ' ' . ($compare === '' ? '' : "[Compare]({$compare})") . ' |';
        }
        return $row . "\n";
    }

    private static function string(\stdClass $object, string $key): string
    {
        return Read::string($object, $key, self::NAME);
    }
}
