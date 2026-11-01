<?php

declare(strict_types=1);

namespace Drupdater\Agent;

use Composer\Plugin\Capability\CommandProvider as CommandProviderCapability;
use Drupdater\Agent\Command\AuditCommand;
use Drupdater\Agent\Command\HandshakeCommand;
use Drupdater\Agent\Command\PatchCheckCommand;
use Drupdater\Agent\Command\PlatformCheckCommand;
use Drupdater\Agent\Command\ProjectCommand;
use Drupdater\Agent\Command\ResolveCommand;

final class CommandProvider implements CommandProviderCapability
{
    public function getCommands(): array
    {
        return [
            new AuditCommand(),
            new HandshakeCommand(),
            new PatchCheckCommand(),
            new PlatformCheckCommand(),
            new ProjectCommand(),
            new ResolveCommand(),
        ];
    }
}
