<?php

declare(strict_types=1);

namespace Drupdater\Agent\Addon;

use Composer\Composer;
use Drupdater\Agent\Failure;
use IonBazan\ComposerDiff\Diff\DiffEntry;
use IonBazan\ComposerDiff\PackageDiff;
use IonBazan\ComposerDiff\Url\GeneratorContainer;

/**
 * composer_diff: the dependency changelog, between the lock the update started from and the
 * working one. Its finding is the report's composer_diff section, production packages first.
 */
final class ComposerDiff implements Addon
{
    public const NAME = 'composer_diff';

    public function handle(array $event, Composer $composer): HookResult
    {
        if (($event['name'] ?? null) !== 'post-composer-update') {
            throw new Failure(Failure::INVALID_REQUEST, sprintf('%s has no hook for %s', self::NAME, json_encode($event['name'] ?? null)));
        }
        $base = $event['base_lock'] ?? null;
        if (!is_array($base)) {
            throw new Failure(Failure::INVALID_REQUEST, 'base_lock must be the composer.lock the update started from');
        }
        $locker = $composer->getLocker();
        if (!$locker->isLocked()) {
            throw new Failure(Failure::FAILED, 'the project has no composer.lock');
        }
        $target = $locker->getLockData();

        $root = $composer->getPackage();
        $direct = array_map('strtolower', array_keys([...$root->getRequires(), ...$root->getDevRequires()]));

        $diff = new PackageDiff();
        $diff->setUrlGenerator(new GeneratorContainer((array) $composer->getConfig()->get('gitlab-domains')));

        $rows = [];
        foreach ([false, true] as $dev) {
            $entries = $diff->getDiff($diff->loadPackagesFromArray($base, $dev, false), $diff->loadPackagesFromArray($target, $dev, false), $direct);
            $section = array_map(static fn (DiffEntry $entry): array => self::row($entry, $dev), iterator_to_array($entries, false));
            usort($section, static fn (array $a, array $b): int => strcmp($a['package'], $b['package']));
            $rows = [...$rows, ...$section];
        }

        return new HookResult(finding: $rows, log: $rows === [] ? [] : ["dependency diff\n" . self::plain($rows)]);
    }

    /**
     * @return array{package: string, operation: string, from?: string, to?: string, dev: bool, direct: bool, compare_url?: string, project_url?: string}
     */
    private static function row(DiffEntry $entry, bool $dev): array
    {
        $row = ['package' => $entry->getPackageName(), 'operation' => $entry->getType()];
        if (($from = $entry->getBaseVersion()) !== null) {
            $row['from'] = $from;
        }
        if (($to = $entry->getTargetVersion()) !== null) {
            $row['to'] = $to;
        }
        $row['dev'] = $dev;
        $row['direct'] = $entry->isDirect();
        if (($compare = $entry->getUrl()) !== null) {
            $row['compare_url'] = $compare;
        }
        if (($project = $entry->getProjectUrl()) !== null) {
            $row['project_url'] = $project;
        }
        return $row;
    }

    /**
     * The log's rendering: one line per package, without the links a terminal can't use.
     *
     * @param list<array<string, mixed>> $rows
     */
    private static function plain(array $rows): string
    {
        $text = '';
        foreach ($rows as $row) {
            $text .= sprintf("%s %s%s: %s → %s\n", $row['operation'], $row['package'], $row['dev'] ? ' (dev)' : '', $row['from'] ?? '-', $row['to'] ?? '-');
        }
        return $text;
    }
}
