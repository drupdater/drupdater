<?php

declare(strict_types=1);

namespace Drupdater\Agent\Config;

use Drupdater\Agent\Addon\Catalogue;
use Drupdater\Agent\Failure;
use Symfony\Component\Yaml\Exception\ParseException;
use Symfony\Component\Yaml\Yaml;

/**
 * A project's .drupdater.yaml, layered over the defaults and checked whole: a file that fails
 * any check is refused, never half applied. Unknown keys are refused so a typo fails loudly.
 */
final class ProjectConfig
{
    public const RUN_TYPES = ['normal', 'security'];

    private const DEFAULT_TIMEOUT = '30m';

    private const LEGACY = <<<'TEXT'
        "addons" and "auto_merge" are now grouped per run type. Replace:
          addons:
            normal: [code_beautifier, ...]
            security: []
          auto_merge:
            normal: false
            security: true
        with:
          run_types:
            normal:
              addons: [code_beautifier, ...]
              auto_merge: false
            security:
              addons: []
              auto_merge: true
        TEXT;

    /** What YAML 1.1 read as booleans, as the binary's YAML library still does. */
    private const BOOLEANS = [
        'yes' => true, 'Yes' => true, 'YES' => true, 'on' => true, 'On' => true, 'ON' => true,
        'no' => false, 'No' => false, 'NO' => false, 'off' => false, 'Off' => false, 'OFF' => false,
    ];

    /**
     * @param list<string> $sites
     * @param array<string, array{addons: list<string>, auto_merge: bool}> $runTypes the addons each
     *        run type runs, mandatory ones first
     */
    private function __construct(
        public readonly bool $found,
        public readonly array $sites,
        public readonly int $timeoutNanoseconds,
        public readonly array $runTypes,
    ) {
    }

    /**
     * A missing file is no error: the defaults apply. So does one with no YAML document in it.
     *
     * @throws Failure FAILED, saying what is wrong with the file
     */
    public static function load(string $path, Catalogue $catalogue): self
    {
        if (!file_exists($path)) {
            return self::resolve(false, [], $catalogue, $path);
        }
        $content = is_file($path) ? @file_get_contents($path) : false;
        if ($content === false) {
            throw new Failure(Failure::FAILED, sprintf('failed to read %s', $path));
        }
        try {
            $document = Yaml::parse($content);
        }
        catch (ParseException $e) {
            throw self::invalid('parsing %s: %s', $path, $e->getMessage());
        }
        if ($document === null) {
            return self::resolve(true, [], $catalogue, $path);
        }
        if (!is_array($document) || (array_is_list($document) && $document !== [])) {
            throw self::invalid('parsing %s: %s', $path, 'the file must be a mapping of keys, such as "sites:"');
        }
        if (array_key_exists('addons', $document) || array_key_exists('auto_merge', $document)) {
            throw self::invalid('in %s: %s', $path, self::LEGACY);
        }
        return self::resolve(true, $document, $catalogue, $path);
    }

    /**
     * @return array{found: bool, sites: list<string>, timeout_ns: int, run_types: array<string, array{addons: list<string>, auto_merge: bool}>}
     */
    public function toArray(): array
    {
        return ['found' => $this->found, 'sites' => $this->sites, 'timeout_ns' => $this->timeoutNanoseconds, 'run_types' => $this->runTypes];
    }

    /**
     * @param array<mixed> $document
     */
    private static function resolve(bool $found, array $document, Catalogue $catalogue, string $path): self
    {
        try {
            $file = new File($document);
            $timeout = $file->scalar('timeout') ?? self::DEFAULT_TIMEOUT;
            $sites = $file->has('sites') ? $file->strings('sites') : ['default'];
            $runTypes = [];
            foreach (self::RUN_TYPES as $type) {
                $block = $file->block('run_types', $type);
                $runTypes[$type] = ['addons' => $block->given('addons') ? $block->strings('addons') : null, 'auto_merge' => $block->bool('auto_merge', self::BOOLEANS)];
            }
            $file->refuseUnknown(['sites', 'timeout', 'run_types']);
        }
        catch (\UnexpectedValueException $e) {
            throw self::invalid('parsing %s: %s', $path, $e->getMessage());
        }

        try {
            $nanoseconds = Duration::parse($timeout);
        }
        catch (\InvalidArgumentException $e) {
            throw self::invalid('in %s: %s', $path, sprintf('invalid timeout "%s" (use a Go duration like "30m" or "2h", or 0 to disable): %s', $timeout, $e->getMessage()));
        }
        // An empty list silently skips every per-site phase, then opens the merge request anyway.
        if ($sites === []) {
            throw self::invalid('in %s: %s', $path, 'no sites configured: "sites" must list at least one Drupal site name');
        }
        foreach ($sites as $i => $site) {
            if ($site === '' || $site === '.' || $site === '..' || str_contains($site, '/')) {
                throw self::invalid('in %s: %s', $path, sprintf('site "%s" is no directory name under sites/', $site));
            }
            if (array_search($site, $sites, true) !== $i) {
                throw self::invalid('in %s: %s', $path, sprintf('site "%s" is listed twice', $site));
            }
        }

        foreach ($runTypes as $runType) {
            foreach ($runType['addons'] ?? [] as $name) {
                if ($catalogue->find($name) === null) {
                    throw new Failure(Failure::FAILED, sprintf('unknown addon "%s" (run "drupdater addons" to list valid names)', $name));
                }
            }
        }
        $resolved = [];
        foreach ($runTypes as $type => $runType) {
            $resolved[$type] = ['addons' => self::addons($catalogue, $runType['addons'], $type), 'auto_merge' => $runType['auto_merge']];
        }
        return new self($found, $sites, $nanoseconds, $resolved);
    }

    /**
     * The mandatory addons, then the configured ones. A normal run that names none gets the
     * catalogue's defaults; a security run stays a focused fix.
     *
     * @param list<string>|null $configured
     * @return list<string>
     */
    private static function addons(Catalogue $catalogue, ?array $configured, string $type): array
    {
        $names = [];
        foreach ($catalogue->entries() as $entry) {
            if ($entry->mandatory) {
                $names[] = $entry->name();
            }
        }
        if ($configured === null && $type === 'normal') {
            foreach ($catalogue->entries() as $entry) {
                if ($entry->enabledByDefault && !$entry->mandatory) {
                    $configured[] = $entry->name();
                }
            }
        }
        foreach ($configured ?? [] as $name) {
            if (!in_array($name, $names, true)) {
                $names[] = $name;
            }
        }
        return $names;
    }

    private static function invalid(string $format, string $path, string $why): Failure
    {
        return new Failure(Failure::FAILED, sprintf($format, $path, $why));
    }
}
