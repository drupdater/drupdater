<?php

declare(strict_types=1);

namespace Drupdater\Agent;

use Composer\Plugin\Capability\CommandProvider as CommandProviderCapability;
use Drupdater\Agent\Command\AddonsCommand;
use Drupdater\Agent\Command\ConfigCommand;
use Drupdater\Agent\Command\ConfigExportCommand;
use Drupdater\Agent\Command\HandshakeCommand;
use Drupdater\Agent\Command\HookCommand;
use Drupdater\Agent\Command\InstallCommand;
use Drupdater\Agent\Command\MergeRequestCommand;
use Drupdater\Agent\Command\PlatformCheckCommand;
use Drupdater\Agent\Command\ProjectCommand;
use Drupdater\Agent\Command\ReportCommand;
use Drupdater\Agent\Command\ResolveCommand;
use Drupdater\Agent\Command\SiteDatabaseCommand;
use Drupdater\Agent\Command\SiteFactsCommand;
use Drupdater\Agent\Command\SiteInstallCommand;
use Drupdater\Agent\Command\SiteRestoreCommand;
use Drupdater\Agent\Command\SiteUpdateCommand;

final class CommandProvider implements CommandProviderCapability
{
    public function getCommands(): array
    {
        return [
            new AddonsCommand(),
            new ConfigCommand(),
            new ConfigExportCommand(),
            new HandshakeCommand(),
            new HookCommand(),
            new InstallCommand(),
            new MergeRequestCommand(),
            new PlatformCheckCommand(),
            new ProjectCommand(),
            new ReportCommand(),
            new ResolveCommand(),
            new SiteDatabaseCommand(),
            new SiteFactsCommand(),
            new SiteInstallCommand(),
            new SiteRestoreCommand(),
            new SiteUpdateCommand(),
        ];
    }
}
