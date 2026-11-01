<?php

declare(strict_types=1);

namespace Drupdater\Agent;

use Composer\Plugin\Capability\CommandProvider as CommandProviderCapability;
use Drupdater\Agent\Command\AddonCommand;
use Drupdater\Agent\Command\AllowPluginsCommand;
use Drupdater\Agent\Command\AuditCommand;
use Drupdater\Agent\Command\HandshakeCommand;
use Drupdater\Agent\Command\PatchCheckCommand;
use Drupdater\Agent\Command\PlatformCheckCommand;
use Drupdater\Agent\Command\ProjectCommand;
use Drupdater\Agent\Command\ResolveCommand;
use Drupdater\Agent\Command\WritePatchesCommand;

final class CommandProvider implements CommandProviderCapability
{
    public function getCommands(): array
    {
        return [
            new AddonCommand(),
            new AllowPluginsCommand(),
            new AuditCommand(),
            new HandshakeCommand(),
            new PatchCheckCommand(),
            new PlatformCheckCommand(),
            new ProjectCommand(),
            new ResolveCommand(),
            new WritePatchesCommand(),
        ];
    }
}
