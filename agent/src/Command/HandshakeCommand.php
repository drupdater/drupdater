<?php

declare(strict_types=1);

namespace Drupdater\Agent\Command;

use Composer\Composer;

/**
 * Lets drupdater confirm the agent is installed and agrees on the protocol before a run depends
 * on it, and names the Composer and PHP it runs under, which a run's outcome depends on.
 *
 * Request: {}. Result: {composer_version, php_version}.
 */
final class HandshakeCommand extends AgentCommand
{
    protected function configure(): void
    {
        $this->setName('drupdater:handshake')
            ->setDescription('Confirms the drupdater agent is installed and speaks drupdater\'s protocol.')
            ->setHidden(true);
    }

    protected function handle(array $request): array
    {
        return ['composer_version' => Composer::getVersion(), 'php_version' => PHP_VERSION];
    }
}
