<?php

declare(strict_types=1);

namespace Drupdater\Agent;

use Composer\Plugin\Capability\CommandProvider as CommandProviderCapability;
use Drupdater\Agent\Command\HandshakeCommand;
use Drupdater\Agent\Command\PatchCheckCommand;
use Drupdater\Agent\Command\ProjectCommand;

final class CommandProvider implements CommandProviderCapability
{
    public function getCommands(): array
    {
        return [new HandshakeCommand(), new PatchCheckCommand(), new ProjectCommand()];
    }
}
