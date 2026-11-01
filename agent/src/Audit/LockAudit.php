<?php

declare(strict_types=1);

namespace Drupdater\Agent\Audit;

use Composer\Advisory\Auditor;
use Composer\Advisory\PartialSecurityAdvisory;
use Composer\Advisory\SecurityAdvisory;
use Composer\Composer;
use Composer\Downloader\TransportException;
use Composer\Package\CompletePackageInterface;
use Composer\Policy\ListPolicyConfig;
use Composer\Policy\PolicyConfig;
use Composer\Repository\RepositorySet;
use Drupdater\Agent\Failure;

/**
 * The audit of composer.lock, with Composer's Auditor deciding what is ignored.
 */
final class LockAudit
{
    public function __construct(private readonly Composer $composer, private readonly PolicyConfig $policy)
    {
    }

    /**
     * Advisories sorted by package, advisory_id, cve and title; abandoned packages by name, with
     * a null replacement when the maintainers named no successor.
     *
     * @return array{advisories: list<array<string, mixed>>, abandoned: list<array{package: string, replacement: string|null}>}
     *
     * @throws Failure when there is no lock, or a repository's advisories cannot be fetched
     */
    public function toArray(): array
    {
        $locker = $this->composer->getLocker();
        if (!$locker->isLocked()) {
            throw new Failure(Failure::FAILED, 'the project has no composer.lock');
        }
        $packages = $locker->getLockedRepository(true)->getPackages();
        if ($packages === []) {
            return ['advisories' => [], 'abandoned' => []];
        }
        $auditor = new Auditor();
        try {
            return [
                'advisories' => $this->advisories($auditor, $packages),
                'abandoned' => $this->abandoned($auditor, $packages),
            ];
        }
        catch (TransportException $e) {
            throw new Failure(Failure::UNOBTAINABLE, 'the security advisories could not be fetched', $e->getMessage(), $e);
        }
    }

    /**
     * @param non-empty-array<\Composer\Package\PackageInterface> $packages
     * @return list<array<string, mixed>>
     */
    private function advisories(Auditor $auditor, array $packages): array
    {
        $repositories = new RepositorySet();
        foreach ($this->composer->getRepositoryManager()->getRepositories() as $repository) {
            $repositories->addRepository($repository);
        }
        $found = $repositories->getMatchingSecurityAdvisories($packages, false, $this->policy->ignoreUnreachable->audit)['advisories'];
        $active = $auditor->processAdvisories(
            $found,
            $this->policy->advisories->getIgnoreListForOperation('audit'),
            $this->policy->advisories->getIgnoreSeverityForOperation('audit'),
        )['advisories'];

        $rows = [];
        foreach ($active as $advisories) {
            foreach ($advisories as $advisory) {
                $rows[] = self::advisory($advisory);
            }
        }
        usort($rows, static fn (array $a, array $b): int => [$a['package'], $a['advisory_id'], (string) $a['cve'], $a['title']]
            <=> [$b['package'], $b['advisory_id'], (string) $b['cve'], $b['title']]);
        return $rows;
    }

    /**
     * @return array<string, mixed>
     */
    private static function advisory(PartialSecurityAdvisory $advisory): array
    {
        if (!$advisory instanceof SecurityAdvisory) {
            throw new \LogicException(sprintf('advisory %s was loaded without its details', $advisory->advisoryId));
        }
        return [
            'package' => $advisory->packageName,
            'advisory_id' => $advisory->advisoryId,
            'title' => $advisory->title,
            'cve' => $advisory->cve,
            'link' => $advisory->link,
            'severity' => $advisory->severity,
            'affected_versions' => $advisory->affectedVersions->getPrettyString(),
            'reported_at' => $advisory->reportedAt->format(DATE_RFC3339),
            'sources' => array_map(
                static fn (array $source): array => ['name' => $source['name'], 'remote_id' => $source['remoteId']],
                array_values($advisory->sources),
            ),
        ];
    }

    /**
     * @param non-empty-array<\Composer\Package\PackageInterface> $packages
     * @return list<array{package: string, replacement: string|null}>
     */
    private function abandoned(Auditor $auditor, array $packages): array
    {
        if ($this->policy->abandoned->audit === ListPolicyConfig::AUDIT_IGNORE) {
            return [];
        }
        $rows = array_map(
            static fn (CompletePackageInterface $package): array => ['package' => $package->getPrettyName(), 'replacement' => $package->getReplacementPackage()],
            array_values($auditor->filterAbandonedPackages($packages, $this->policy->abandoned->getFlatIgnoreForOperation('audit'))),
        );
        usort($rows, static fn (array $a, array $b): int => $a['package'] <=> $b['package']);
        return $rows;
    }
}
