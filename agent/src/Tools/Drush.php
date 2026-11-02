<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tools;

use Drupdater\Agent\Failure;
use Drupdater\Agent\Process\ProcessResult;
use Drupdater\Agent\Process\ProcessRunner;

/**
 * The project's drush, run against one site of a multisite, with drupdater's own drush commands
 * (resources/drush) loaded through --include.
 */
class Drush
{
    public function __construct(
        private readonly ProcessRunner $runner,
        private readonly Bin $bin,
        private readonly string $projectDir,
        private readonly string $include = '',
    ) {
    }

    /** drupdater's drush commands, shipped with the agent. */
    public static function include(): string
    {
        return dirname(__DIR__, 2) . '/resources/drush';
    }

    /**
     * Imports the site's translations through locale_deploy.
     *
     * @throws Failure when drush fails
     */
    public function localizeTranslations(string $site): void
    {
        $this->run($site, 'locale-deploy:localize-translations');
    }

    /**
     * What drupdater reads from the site, in one bootstrap of drush's update kernel: the code is
     * already updated, the schema is not. Without $updateStatus drupal.org is not asked which
     * modules are unsupported.
     *
     * @throws Failure when drush fails or prints no JSON object
     */
    public function facts(string $site, bool $updateStatus): \stdClass
    {
        $args = [...$this->included(), 'drupdater:facts'];
        if (!$updateStatus) {
            $args[] = '--skip-update-status';
        }
        $result = $this->run($site, ...$args);
        $facts = self::decode('drush drupdater:facts', $result);
        if (!$facts instanceof \stdClass) {
            throw new Failure(Failure::FAILED, 'drush drupdater:facts printed no JSON object', trim($result->stdout));
        }
        return $facts;
    }

    /** @throws Failure when drush fails */
    public function updateDatabase(string $site): void
    {
        $this->run($site, 'updatedb', '--yes');
    }

    /**
     * Saves every configuration object again, so the export carries the update's new defaults.
     *
     * @throws Failure when drush fails
     */
    public function resaveConfig(string $site): void
    {
        $this->run($site, ...$this->included(), ...['drupdater:config-resave']);
    }

    /** @throws Failure when drush fails */
    public function exportConfig(string $site): void
    {
        $this->run($site, 'config:export', '--yes');
    }

    /** @throws Failure when drush fails */
    public function installSite(string $site): void
    {
        $this->run($site, '--existing-config', '--yes', 'site:install', '--sites-subdir=' . $site, '--verbose');
    }

    /**
     * The site's absolute config sync directory. Unlike facts() it needs no database, so it
     * answers before the site is installed.
     *
     * @throws Failure when drush fails or the site has none
     */
    public function configSyncDir(string $site): string
    {
        $result = $this->run($site, 'status', '--fields=config-sync,root', '--format=json');
        $status = self::decode('drush status', $result);
        $sync = $status instanceof \stdClass ? $status->{'config-sync'} ?? '' : '';
        if (!is_string($sync) || $sync === '') {
            throw new Failure(Failure::FAILED, sprintf('site %s has no config sync directory', $site), 'drush status: ' . trim($result->stdout));
        }
        if (str_starts_with($sync, '/')) {
            return $sync;
        }
        $root = $status->root ?? '';
        return (is_string($root) ? $root : '') . '/' . $sync;
    }

    /** @return list<string> */
    private function included(): array
    {
        return $this->include === '' ? [] : ['--include=' . $this->include];
    }

    private function run(string $site, string ...$args): ProcessResult
    {
        // The project's settings.php picks the site from SITE_NAME.
        $result = $this->runner->run([...$this->bin->command('drush'), ...array_values($args)], $this->projectDir, ['SITE_NAME' => $site] + $this->bin->env());
        if (!$result->succeeded()) {
            $named = array_filter($args, static fn (string $arg): bool => !str_starts_with($arg, '--include='));
            throw ToolFailure::exited('drush ' . implode(' ', $named), $result);
        }
        return $result;
    }

    private static function decode(string $tool, ProcessResult $result): mixed
    {
        try {
            return json_decode($result->stdout, false, 512, JSON_THROW_ON_ERROR);
        }
        catch (\JsonException $e) {
            throw ToolFailure::unreadable($tool, $e);
        }
    }
}
