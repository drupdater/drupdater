<?php

declare(strict_types=1);

namespace Drupdater\Agent\Command;

use Drupdater\Agent\Failure;
use Symfony\Component\Console\Input\ArrayInput;

/**
 * Brings vendor/ to the lock with Composer's own install command, run in this process: plugins
 * and scripts run as in any `composer install`.
 *
 * Request: {}. Result: {}.
 */
final class InstallCommand extends AgentCommand
{
    protected function configure(): void
    {
        $this->setName('drupdater:install')
            ->setDescription('Installs the project\'s dependencies as its lock names them.')
            ->setHidden(true);
    }

    protected function handle(array $request): array
    {
        $install = $this->getApplication()?->find('install');
        if ($install === null) {
            throw new Failure(Failure::INTERNAL, 'Composer has no install command');
        }
        // drupdater checks the platform itself, against the PHP the sites will run on.
        $input = new ArrayInput(['--no-progress' => true, '--optimize-autoloader' => true, '--ignore-platform-reqs' => true]);
        $input->setInteractive(false);
        try {
            $status = $install->run($input, $this->output ?? throw new \LogicException('no output'));
        }
        // A download or a plugin can fail the install by throwing; either is the project's failure.
        catch (\Exception $e) {
            throw new Failure(Failure::FAILED, 'composer install failed', $e->getMessage(), $e);
        }
        if ($status !== 0) {
            throw new Failure(Failure::FAILED, 'composer install failed', sprintf('exit code %d', $status));
        }
        return [];
    }
}
