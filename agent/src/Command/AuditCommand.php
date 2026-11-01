<?php

declare(strict_types=1);

namespace Drupdater\Agent\Command;

use Composer\Downloader\TransportException;
use Drupdater\Agent\Audit\LockAudit;
use Drupdater\Agent\Failure;

/**
 * Audits the locked packages, as `composer audit --locked` does, under the project's own audit
 * policy: its ignore list, ignored severities and abandoned setting.
 *
 * Request: {}. Result: {advisories[], abandoned[{package, replacement}]}, both sorted; see
 * LockAudit. A repository whose advisories cannot be fetched is an "unobtainable" failure.
 */
final class AuditCommand extends AgentCommand
{
    protected function configure(): void
    {
        $this->setName('drupdater:audit')
            ->setDescription('Reports the security advisories and abandoned packages in composer.lock.')
            ->setHidden(true);
    }

    protected function handle(array $request): array
    {
        $composer = $this->requireComposer();
        if (!$composer->getLocker()->isLocked()) {
            throw new Failure(Failure::FAILED, 'the project has no composer.lock');
        }
        try {
            return (new LockAudit($composer, $this->createPolicyConfig($composer->getConfig(), null)))->toArray();
        }
        catch (TransportException $e) {
            throw new Failure(Failure::UNOBTAINABLE, 'the security advisories could not be fetched', $e->getMessage(), $e);
        }
    }
}
