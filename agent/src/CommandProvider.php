<?php

declare(strict_types=1);

namespace Drupdater\Agent;

use Composer\Plugin\Capability\CommandProvider as CommandProviderCapability;
use Drupdater\Agent\Command\AddonsCommand;
use Drupdater\Agent\Command\HandshakeCommand;
use Drupdater\Agent\Command\HookCommand;
use Drupdater\Agent\Command\PlatformCheckCommand;
use Drupdater\Agent\Command\ProjectCommand;
use Drupdater\Agent\Command\ReportCommand;
use Drupdater\Agent\Command\ResolveCommand;

final class CommandProvider implements CommandProviderCapability
{
    public function getCommands(): array
    {
        return [
            new AddonsCommand(),
            new HandshakeCommand(),
            new HookCommand(),
            new PlatformCheckCommand(),
            new ProjectCommand(),
            new ReportCommand(),
            new ResolveCommand(),
        ];
    }
}
