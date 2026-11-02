<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tools;

use Drupdater\Agent\Failure;
use Drupdater\Agent\Process\ProcessRunner;

/**
 * The project's drush, run against one site of a multisite.
 */
class Drush
{
    public function __construct(private readonly ProcessRunner $runner, private readonly Bin $bin, private readonly string $projectDir)
    {
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

    private function run(string $site, string ...$args): void
    {
        // The project's settings.php picks the site from SITE_NAME.
        $result = $this->runner->run([...$this->bin->command('drush'), ...$args], $this->projectDir, ['SITE_NAME' => $site] + $this->bin->env());
        if (!$result->succeeded()) {
            throw ToolFailure::exited('drush ' . implode(' ', $args), $result);
        }
    }
}
