<?php

declare(strict_types=1);

namespace Drupdater\Agent\Command;

use Drupdater\Agent\Failure;
use Drupdater\Agent\Platform\PlatformCheck;

/**
 * Reports the platform requirements of the locked packages and the root package that this PHP
 * does not meet, as `composer check-platform-reqs --lock` decides them.
 *
 * Request: {}. Result: {unmet[{name, version, constraint, required_by}]}, version null when the
 * platform package is missing altogether.
 */
final class PlatformCheckCommand extends AgentCommand
{
    public function __construct(private readonly PlatformCheck $check = new PlatformCheck())
    {
        parent::__construct();
    }

    protected function configure(): void
    {
        $this->setName('drupdater:platform-check')
            ->setDescription('Reports the lock\'s platform requirements this PHP does not meet.')
            ->setHidden(true);
    }

    protected function handle(array $request): array
    {
        $composer = $this->requireComposer();
        if (!$composer->getLocker()->isLocked()) {
            throw new Failure(Failure::FAILED, 'the project has no composer.lock');
        }
        return ['unmet' => $this->check->unmet($composer)];
    }
}
