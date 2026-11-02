<?php

declare(strict_types=1);

namespace Drupdater\Agent\Addon;

use Drupdater\Agent\Failure;
use Drupdater\Agent\Log;
use Drupdater\Agent\Project\Project;
use Drupdater\Agent\Tools\Phpcs;

/**
 * code_beautifier: fixes the coding standard violations PHPCBF can in what the project's
 * phpcs.xml covers, writing a phpcs.xml over the custom code where the project has none.
 */
final class CodeBeautifier implements Addon
{
    public const COMMIT_MESSAGE = 'Update coding styles';
    public const CONFIG_COMMIT_MESSAGE = 'Add PHPCS config';

    private const CONFIG_FILES = ['phpcs.xml', 'phpcs.xml.dist'];

    /**
     * @param \Closure(): Project $project
     * @param Phpcs $projectPhpcs the project's own, for a project with drupal/coder
     */
    public function __construct(
        private readonly Log $log,
        private readonly \Closure $project,
        private readonly Phpcs $projectPhpcs,
        private readonly Phpcs $bundledPhpcs,
    ) {
    }

    public static function name(): string
    {
        return 'code_beautifier';
    }

    /** Format: after deprecations_remover's rewrites, which it puts into the project's style. */
    public static function hooks(): array
    {
        return [EventName::PostCodeUpdate->value => Stage::Format];
    }

    public static function create(Services $services): static
    {
        return new self($services->log, $services->project(...), $services->projectPhpcs(), $services->bundledPhpcs());
    }

    /**
     * Whether the fixes landed is up to the commit: phpcbf may have changed nothing even where
     * phpcs reported something fixable. So the report reads it from the commit.
     */
    public function handle(Event $event): HookResult
    {
        $this->log->info('updating coding styles');

        $commits = [];
        $config = self::configFile($event->projectDir);
        if ($config === null) {
            if (!$this->createConfig($event->projectDir)) {
                $this->log->debug('no phpcs.xml created, skipping coding style update');
                return new HookResult();
            }
            $commits[] = new Commit(self::CONFIG_COMMIT_MESSAGE, ['phpcs.xml']);
        }
        elseif (!self::hasPathDefinitions($config)) {
            $this->log->warning('phpcs.xml found but no file path definitions, skipping coding style update');
            return new HookResult();
        }

        $phpcs = $this->phpcs();
        $report = $phpcs->report();
        if ($report['totals']['fixable'] === 0) {
            $this->log->debug('no coding style issues found');
            return new HookResult(null, $commits);
        }

        try {
            $phpcs->fix();
        }
        catch (Failure $failure) {
            $this->log->debug('remaining issues', ['error' => $failure->reason . ': ' . $failure->getMessage()]);
        }

        $this->log->debug('adding files to commit', ['files' => $report['files']]);
        $files = [];
        foreach ($report['files'] as $file => $counts) {
            if ($counts['errors'] + $counts['warnings'] === 0) {
                continue;
            }
            $files[] = ltrim(str_starts_with($file, $event->projectDir) ? substr($file, strlen($event->projectDir)) : $file, '/');
        }
        sort($files, SORT_STRING);

        $commits[] = new Commit(self::COMMIT_MESSAGE, $files);
        return new HookResult(['files' => $files, 'fixable' => $report['totals']['fixable']], $commits);
    }

    /** The fixes, only once their commit was made: otherwise they never reached the branch. */
    public static function report(Findings $findings): mixed
    {
        $recorded = $findings->shared(EventName::PostCodeUpdate);
        if ($recorded === null || !$recorded->committed(self::COMMIT_MESSAGE)) {
            return null;
        }
        return $recorded->finding();
    }

    /** The project's own coder brings the version and the standards its phpcs.xml was written for. */
    private function phpcs(): Phpcs
    {
        try {
            return ($this->project)()->installed('drupal/coder') ? $this->projectPhpcs : $this->bundledPhpcs;
        }
        catch (Failure) {
            return $this->bundledPhpcs;
        }
    }

    /** An unreadable config counts as found: only absence means there is none. */
    private static function configFile(string $projectDir): ?string
    {
        foreach (self::CONFIG_FILES as $name) {
            if (file_exists($projectDir . '/' . $name)) {
                return $projectDir . '/' . $name;
            }
        }
        return null;
    }

    /**
     * @throws Failure when the config cannot be read or parsed
     */
    private static function hasPathDefinitions(string $config): bool
    {
        $contents = @file_get_contents($config);
        if ($contents === false) {
            throw new Failure(Failure::FAILED, 'failed to read phpcs config', $config);
        }
        $previous = libxml_use_internal_errors(true);
        try {
            $ruleset = simplexml_load_string($contents, options: LIBXML_NONET);
        }
        finally {
            libxml_clear_errors();
            libxml_use_internal_errors($previous);
        }
        if ($ruleset === false || $ruleset->getName() !== 'ruleset') {
            throw new Failure(Failure::FAILED, 'failed to parse phpcs config', $config);
        }
        return $ruleset->file->count() > 0;
    }

    /**
     * Writes a phpcs.xml covering the custom code, for the hook to commit.
     */
    private function createConfig(string $projectDir): bool
    {
        $this->log->debug('no phpcs.xml or phpcs.xml.dist file found, creating phpcs.xml');
        $project = ($this->project)();
        if ($project->customCodeDirectories === []) {
            $this->log->debug('no custom code directories found, skipping coding style update');
            return false;
        }
        $coreMajor = explode('.', $project->version('drupal/core'), 2)[0];
        if (@file_put_contents($projectDir . '/phpcs.xml', self::config($project->customCodeDirectories, $coreMajor)) === false) {
            throw new Failure(Failure::FAILED, 'failed to write phpcs.xml', $projectDir . '/phpcs.xml');
        }
        return true;
    }

    /**
     * @param list<string> $directories
     */
    public static function config(array $directories, string $coreMajor): string
    {
        $files = implode('', array_map(static fn (string $directory): string => "\n    <file>" . $directory . '</file>', $directories));
        return <<<XML
            <?xml version="1.0" encoding="UTF-8"?>
            <ruleset name="drupal-updater">
                <description>PHP CodeSniffer configuration generated by Drupal Updater</description>{$files}
                <arg name="extensions" value="php,module,inc,install,test,profile,theme"/>
                <config name="drupal_core_version" value="{$coreMajor}"/>
                <rule ref="Drupal"/>
                <rule ref="DrupalPractice"/>
            </ruleset>

            XML;
    }
}
