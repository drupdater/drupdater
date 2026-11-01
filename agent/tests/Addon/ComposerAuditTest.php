<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\Addon;

use Drupdater\Agent\Addon\AbortRun;
use Drupdater\Agent\Addon\ComposerAudit;
use Drupdater\Agent\Addon\Event;
use Drupdater\Agent\Addon\Plan;
use Drupdater\Agent\Log;
use PHPUnit\Framework\TestCase;

final class ComposerAuditTest extends TestCase
{
    public function testANormalRunRecordsTheAuditInTheReportsNamesAndProposesNothing(): void
    {
        $result = self::audit([self::lockAdvisory('drupal/core', 'CVE-2023-1234')], [['package' => 'old/lib', 'replacement' => null]])
            ->handle(Event::preComposerUpdate('/p', new Plan()));

        self::assertNull($result->plan);
        self::assertSame([
            'advisories' => [[
                'reportedAt' => '2025-01-29T06:52:00+00:00',
                'severity' => 'high',
                'advisoryId' => 'PKSA-drupal/core',
                'cve' => 'CVE-2023-1234',
                'sources' => [['name' => 'GitHub', 'remoteId' => 'GHSA-1']],
                'link' => 'https://example.com/drupal/core',
                'packageName' => 'drupal/core',
                'affectedVersions' => '<1.1.0',
                'title' => 'Issue in drupal/core',
            ]],
            'abandoned' => [['packageName' => 'old/lib', 'replacement' => '']],
        ], $result->finding);
    }

    public function testAnAdvisoryWithoutACveRecordsItAsEmpty(): void
    {
        $advisory = self::lockAdvisory('a/b', null);

        $finding = self::audit([$advisory])->handle(Event::preComposerUpdate('/p', new Plan()))->finding;

        self::assertSame('', $finding['advisories'][0]['cve']);
    }

    public function testASecurityRunRestrictsTheUpdateToTheAffectedPackagesWithCoresCompanions(): void
    {
        $result = self::audit([self::lockAdvisory('drupal/core', 'CVE-1'), self::lockAdvisory('other/package', 'CVE-2'), self::lockAdvisory('other/package', 'CVE-3')])
            ->handle(Event::preComposerUpdate('/p', new Plan(), security: true));

        self::assertSame(['only' => ['drupal/core', 'other/package', 'drupal/core-recommended', 'drupal/core-composer-scaffold'], 'keep' => [], 'minimal_changes' => true], $result->plan?->toArray());
    }

    public function testASecurityRunWithoutCoreAddsNoCompanions(): void
    {
        $result = self::audit([self::lockAdvisory('other/package', 'CVE-2')])->handle(Event::preComposerUpdate('/p', new Plan(), security: true));

        self::assertSame(['other/package'], $result->plan?->only);
    }

    public function testASecurityRunWithNothingToFixEndsTheRun(): void
    {
        $this->expectExceptionObject(new AbortRun('No security advisories found'));
        self::audit([])->handle(Event::preComposerUpdate('/p', new Plan(), security: true));
    }

    public function testTheAuditAfterTheUpdateLogsWhatItFixedAgainstTheOneBefore(): void
    {
        $log = new Log();
        $before = (new ComposerAudit(new Log(), static fn (): array => ['advisories' => [self::lockAdvisory('a/a', 'CVE-1'), self::lockAdvisory('b/b', 'CVE-2')], 'abandoned' => []]))
            ->handle(Event::preComposerUpdate('/p', new Plan()))->finding;

        $after = (new ComposerAudit($log, static fn (): array => ['advisories' => [self::lockAdvisory('b/b', 'CVE-2')], 'abandoned' => [['package' => 'drupal/old', 'replacement' => null]]]))
            ->handle(Event::postCodeUpdate('/p', earlier: Records::findings([Records::record('pre-composer-update', $before)])));

        self::assertSame(['b/b'], array_column($after->finding['advisories'], 'packageName'));
        self::assertSame([['level' => 'info', 'message' => 'security advisories', 'context' => ['fixed' => 1, 'unresolved' => 1, 'abandoned' => 0]]], $log->entries());
    }

    public function testTheReportComparesTheTwoAudits(): void
    {
        $fixed = self::advisory('a/a', 'CVE-1');
        $open = self::advisory('b/b', 'CVE-2');

        $section = ComposerAudit::report(Records::findings([
            Records::record('pre-composer-update', ['advisories' => [$fixed, $open], 'abandoned' => []]),
            Records::record('post-code-update', ['advisories' => [$open], 'abandoned' => [
                ['packageName' => 'swiftmailer/swiftmailer', 'replacement' => 'symfony/mailer'],
                ['packageName' => 'drupal/old', 'replacement' => ''],
            ]]),
        ]));

        self::assertSame([
            'fixed' => [$fixed],
            'remaining' => [$open],
            'abandoned' => [['packageName' => 'swiftmailer/swiftmailer', 'replacement' => 'symfony/mailer']],
        ], $section);
    }

    public function testTheReportHasEveryListEvenAnEmptyOne(): void
    {
        $section = ComposerAudit::report(Records::findings([
            Records::record('pre-composer-update', ['advisories' => [self::advisory('a/a', 'CVE-1')], 'abandoned' => []]),
            Records::record('post-code-update', ['advisories' => [], 'abandoned' => []]),
        ]));

        self::assertSame([], $section['remaining']);
        self::assertSame([], $section['abandoned']);
    }

    public function testNoReportWithoutTheAuditAfterTheUpdateOrWithNothingToSay(): void
    {
        $before = Records::record('pre-composer-update', ['advisories' => [self::advisory('a/a', 'CVE-1')], 'abandoned' => []]);

        self::assertNull(ComposerAudit::report(Records::findings([$before])), 'every advisory found before would read as fixed');
        self::assertNull(ComposerAudit::report(Records::findings([Records::record('post-code-update', ['advisories' => [], 'abandoned' => [['packageName' => 'drupal/x', 'replacement' => '']]])])));
    }

    public function testFixedAdvisoriesAreTheOnesGoneAfterTheUpdateInTheirOrderBefore(): void
    {
        $a = self::advisory('a/a', 'CVE-1');
        $b = self::advisory('b/b', '');
        $c = self::advisory('c/c', 'CVE-3');

        self::assertSame([$a, $c], ComposerAudit::fixed([$a, $b, $c], [$b]));
        self::assertSame([], ComposerAudit::fixed([$a, $b], [$a, $b]), 'nothing changed, nothing fixed');
        self::assertSame([$a, $b], ComposerAudit::fixed([$a, $b], []), 'everything gone, everything fixed');
    }

    public function testAdvisoriesWithoutACveDoNotCollide(): void
    {
        $first = ['cve' => '', 'advisoryId' => '', 'packageName' => 'a/a', 'title' => 'One'];
        $second = ['cve' => '', 'advisoryId' => '', 'packageName' => 'a/a', 'title' => 'Two'];

        self::assertSame([$first], ComposerAudit::fixed([$first, $second], [$second]));
    }

    public function testTheKeyFallsBackFromCveToAdvisoryIdToPackageAndTitle(): void
    {
        self::assertSame('cve:CVE-1', ComposerAudit::key(['cve' => 'CVE-1', 'advisoryId' => 'X']));
        self::assertSame('id:X', ComposerAudit::key(['cve' => '', 'advisoryId' => 'X']));
        self::assertNotSame(
            ComposerAudit::key(['packageName' => 'a', 'title' => 'b"c']),
            ComposerAudit::key(['packageName' => 'a"b', 'title' => 'c']),
            'a separator in free text cannot make two advisories one',
        );
    }

    /**
     * @param list<array<string, mixed>> $advisories
     * @param list<array{package: string, replacement: string|null}> $abandoned
     */
    private static function audit(array $advisories, array $abandoned = []): ComposerAudit
    {
        return new ComposerAudit(new Log(), static fn (): array => ['advisories' => $advisories, 'abandoned' => $abandoned]);
    }

    /**
     * An advisory as LockAudit reports it.
     *
     * @return array<string, mixed>
     */
    private static function lockAdvisory(string $package, ?string $cve): array
    {
        return [
            'package' => $package,
            'advisory_id' => 'PKSA-' . $package,
            'title' => 'Issue in ' . $package,
            'cve' => $cve,
            'link' => 'https://example.com/' . $package,
            'severity' => 'high',
            'affected_versions' => '<1.1.0',
            'reported_at' => '2025-01-29T06:52:00+00:00',
            'sources' => [['name' => 'GitHub', 'remote_id' => 'GHSA-1']],
        ];
    }

    /**
     * An advisory as the finding and the report name it.
     *
     * @return array<string, mixed>
     */
    private static function advisory(string $package, string $cve): array
    {
        return ['reportedAt' => '', 'severity' => 'high', 'advisoryId' => 'PKSA-' . $package, 'cve' => $cve, 'sources' => [], 'link' => '', 'packageName' => $package, 'affectedVersions' => '', 'title' => $package];
    }
}
