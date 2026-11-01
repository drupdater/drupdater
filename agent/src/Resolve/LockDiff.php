<?php

declare(strict_types=1);

namespace Drupdater\Agent\Resolve;

use Composer\Package\Loader\ArrayLoader;
use Composer\Package\PackageInterface;
use Composer\Package\Version\VersionParser;

/**
 * What changed between two composer.lock files, package by package.
 */
final class LockDiff
{
    /** The order changes come back in, which the report and the merge request carry. */
    private const ACTION_ORDER = ['Upgrade', 'Downgrade', 'Remove', 'Install'];

    /**
     * @param array<string, mixed> $before
     * @param array<string, mixed> $after
     * @return list<array{action: string, package: string, from: string, to: string}>
     */
    public static function between(array $before, array $after): array
    {
        $old = self::packages($before);
        $new = self::packages($after);

        $changes = [];
        foreach ($new as $name => $package) {
            $previous = $old[$name] ?? null;
            if ($previous === null) {
                $changes[] = self::change('Install', $name, '', $package->getFullPrettyVersion());
                continue;
            }
            if (!self::differ($previous, $package)) {
                continue;
            }
            // Composer's own rule, which counts a new commit of a branch as an upgrade.
            $action = VersionParser::isUpgrade($previous->getVersion(), $package->getVersion()) ? 'Upgrade' : 'Downgrade';
            $changes[] = self::change($action, $name, $previous->getFullPrettyVersion(), $package->getFullPrettyVersion());
        }
        foreach ($old as $name => $package) {
            if (!isset($new[$name])) {
                $changes[] = self::change('Remove', $name, $package->getFullPrettyVersion(), '');
            }
        }

        usort($changes, static fn (array $a, array $b): int => [array_search($a['action'], self::ACTION_ORDER, true), $a['package']]
            <=> [array_search($b['action'], self::ACTION_ORDER, true), $b['package']]);
        return $changes;
    }

    /**
     * Dev and non-dev packages together, by name: a package that only moved between require and
     * require-dev did not change.
     *
     * @param array<string, mixed> $lock
     * @return array<string, PackageInterface>
     */
    private static function packages(array $lock): array
    {
        $loader = new ArrayLoader(null, true);
        $packages = [];
        foreach ([...($lock['packages'] ?? []), ...($lock['packages-dev'] ?? [])] as $config) {
            $package = $loader->load($config);
            $packages[$package->getName()] = $package;
        }
        return $packages;
    }

    private static function differ(PackageInterface $a, PackageInterface $b): bool
    {
        return $a->getVersion() !== $b->getVersion()
            || $a->getSourceReference() !== $b->getSourceReference()
            || $a->getDistReference() !== $b->getDistReference();
    }

    /**
     * @return array{action: string, package: string, from: string, to: string}
     */
    private static function change(string $action, string $package, string $from, string $to): array
    {
        return ['action' => $action, 'package' => $package, 'from' => $from, 'to' => $to];
    }
}
