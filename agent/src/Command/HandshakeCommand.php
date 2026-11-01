<?php

declare(strict_types=1);

namespace Drupdater\Agent\Command;

/**
 * Answers with an empty result, so drupdater can confirm the agent is installed and agrees on
 * the protocol before a run depends on it.
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
        return [];
    }
}
