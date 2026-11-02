<?php

declare(strict_types=1);

namespace Drupdater\Agent\Addon;

use Composer\Composer;
use Drupdater\Agent\Failure;
use Drupdater\Agent\Log;
use IonBazan\ComposerDiff\Diff\DiffEntry;
use IonBazan\ComposerDiff\PackageDiff;
use IonBazan\ComposerDiff\Url\GeneratorContainer;

/**
 * composer_diff: the dependency changelog, between the lock the update started from and the
 * working one. Its finding is the report's composer_diff section, production packages first.
 */
final class ComposerDiff implements Addon
{
    public function __construct(private readonly Log $log, private readonly Composer $composer)
    {
    }

    public static function name(): string
    {
        return 'composer_diff';
    }

    /** Inspect: every other post-composer-update hook has finished with the lock by then. */
    public static function hooks(): array
    {
        return [EventName::PostComposerUpdate->value => Stage::Inspect];
    }

    public static function create(Services $services): static
    {
        return new self($services->log, $services->composer());
    }

    public function handle(Event $event): HookResult
    {
        $base = $event->baseLock();
        $composer = $this->composer;
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

        if ($rows !== []) {
            $this->log->info("dependency diff\n" . self::plain($rows));
        }
        return new HookResult($rows);
    }

    /** The rows as found: production packages first, each half by name. */
    public static function report(Findings $findings): mixed
    {
        $rows = $findings->shared(EventName::PostComposerUpdate)?->finding();
        return is_array($rows) && $rows !== [] ? $rows : null;
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
