<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\Audit;

use Composer\Policy\PolicyConfig;
use Drupdater\Agent\Audit\LockAudit;
use Drupdater\Agent\Failure;
use Drupdater\Agent\Tests\InProject;
use PHPUnit\Framework\TestCase;

/**
 * The audit of composer.lock, against an offline project whose package repository carries its
 * own security advisories.
 */
final class LockAuditTest extends TestCase
{
    use InProject;

    private const ADVISORY_RCE = [
        'advisoryId' => 'PKSA-aaaa-bbbb-cccc',
        'packageName' => 'fixture/a',
        'affectedVersions' => '<1.1.0',
        'title' => 'Remote code execution',
        'cve' => 'CVE-2025-0001',
        'link' => 'https://example.com/advisories/CVE-2025-0001',
        'reportedAt' => '2025-01-29T06:52:00+00:00',
        'sources' => [
            ['name' => 'GitHub', 'remoteId' => 'GHSA-aaaa-bbbb-cccc'],
            ['name' => 'FriendsOfPHP/security-advisories', 'remoteId' => 'fixture/a/CVE-2025-0001.yaml'],
        ],
        'severity' => 'high',
    ];

    private const ADVISORY_XSS = [
        'advisoryId' => 'PKSA-dddd-eeee-ffff',
        'packageName' => 'fixture/a',
        'affectedVersions' => '>=1.0.0,<1.0.5',
        'title' => 'Cross-site scripting',
        'reportedAt' => '2025-02-01T00:00:00+00:00',
        'sources' => [['name' => 'GitHub', 'remoteId' => 'GHSA-dddd-eeee-ffff']],
    ];

    private const ADVISORY_OLDER = [
        'advisoryId' => 'PKSA-gggg-hhhh-iiii',
        'packageName' => 'fixture/a',
        'affectedVersions' => '<0.9',
        'title' => 'Fixed long ago',
        'reportedAt' => '2020-01-01T00:00:00+00:00',
        'sources' => [['name' => 'GitHub', 'remoteId' => 'GHSA-gggg-hhhh-iiii']],
    ];

    private const AUDIT = [
        'advisories' => [
            [
                'package' => 'fixture/a',
                'advisory_id' => 'PKSA-aaaa-bbbb-cccc',
                'title' => 'Remote code execution',
                'cve' => 'CVE-2025-0001',
                'link' => 'https://example.com/advisories/CVE-2025-0001',
                'severity' => 'high',
                'affected_versions' => '<1.1.0',
                'reported_at' => '2025-01-29T06:52:00+00:00',
                'sources' => [
                    [
                        'name' => 'GitHub',
                        'remote_id' => 'GHSA-aaaa-bbbb-cccc',
                    ],
                    [
                        'name' => 'FriendsOfPHP/security-advisories',
                        'remote_id' => 'fixture/a/CVE-2025-0001.yaml',
                    ],
                ],
            ],
            [
                'package' => 'fixture/a',
                'advisory_id' => 'PKSA-dddd-eeee-ffff',
                'title' => 'Cross-site scripting',
                'cve' => null,
                'link' => null,
                'severity' => null,
                'affected_versions' => '>=1.0.0,<1.0.5',
                'reported_at' => '2025-02-01T00:00:00+00:00',
                'sources' => [
                    [
                        'name' => 'GitHub',
                        'remote_id' => 'GHSA-dddd-eeee-ffff',
                    ],
                ],
            ],
        ],
        'abandoned' => [
            [
                'package' => 'fixture/gone',
                'replacement' => null,
            ],
            [
                'package' => 'fixture/old',
                'replacement' => 'fixture/new',
            ],
        ],
    ];

    public function testAuditsTheLockSortedAndWithTheReplacementOfEachAbandonedPackage(): void
    {
        self::assertSame(self::AUDIT, $this->audit($this->project()));
    }

    public function testLeavesOutWhatTheProjectIgnores(): void
    {
        $audit = $this->audit($this->project(['audit' => ['ignore' => ['CVE-2025-0001'], 'abandoned' => 'ignore']]));

        self::assertSame(['PKSA-dddd-eeee-ffff'], array_column($audit['advisories'], 'advisory_id'));
        self::assertSame([], $audit['abandoned']);
    }

    public function testLeavesOutAnIgnoredSeverity(): void
    {
        $audit = $this->audit($this->project(['audit' => ['ignore-severity' => ['high']]]));

        self::assertSame(['PKSA-dddd-eeee-ffff'], array_column($audit['advisories'], 'advisory_id'));
    }

    public function testFindsNothingInAnEmptyLock(): void
    {
        $project = $this->project();
        $this->writeFiles($project, ['composer.lock' => json_encode(['packages' => [], 'packages-dev' => []])]);

        self::assertSame(['advisories' => [], 'abandoned' => []], $this->audit($project));
    }

    public function testRefusesAProjectWithoutALock(): void
    {
        $project = $this->project();
        unlink($project . '/composer.lock');

        $this->expectExceptionObject(new Failure(Failure::FAILED, 'the project has no composer.lock'));
        $this->audit($project);
    }

    /**
     * @return array<string, mixed>
     */
    private function audit(string $project): array
    {
        $composer = $this->composer($project);
        return (new LockAudit($composer, PolicyConfig::fromConfig($composer->getConfig())))->toArray();
    }

    /**
     * @param array<string, mixed> $config
     * @param list<array<string, mixed>> $packages extra locked packages
     */
    private function project(array $config = [], array $packages = []): string
    {
        $locked = [
            ['name' => 'fixture/a', 'version' => '1.0.0', 'type' => 'library', 'require' => ['ext-drupdaterfixture' => '*']],
            ['name' => 'fixture/old', 'version' => '1.0.0', 'type' => 'library', 'abandoned' => 'fixture/new'],
            ['name' => 'fixture/gone', 'version' => '1.0.0', 'type' => 'library', 'abandoned' => true],
            ['name' => 'fixture/safe', 'version' => '2.0.0', 'type' => 'library', 'require' => ['php' => '>=7.4 <9']],
            ...$packages,
        ];
        $project = $this->scratchDir();
        $this->writeFiles($project, [
            'composer.json' => json_encode([
                'name' => 'fixture/site',
                'repositories' => [
                    ['type' => 'package', 'package' => $locked, 'security-advisories' => [
                        'fixture/a' => [self::ADVISORY_XSS, self::ADVISORY_OLDER, self::ADVISORY_RCE],
                    ]],
                    ['packagist.org' => false],
                ],
                'require' => ['php' => '>=8.3'] + array_fill_keys(array_column($locked, 'name'), '*'),
                'config' => (object) $config,
            ], JSON_UNESCAPED_SLASHES),
            'composer.lock' => json_encode(['packages' => $locked, 'packages-dev' => []], JSON_UNESCAPED_SLASHES),
        ]);
        return $project;
    }

}
