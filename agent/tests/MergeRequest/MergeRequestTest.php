<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\MergeRequest;

use Drupdater\Agent\Failure;
use Drupdater\Agent\Json;
use Drupdater\Agent\MergeRequest\MergeRequest;
use Drupdater\Agent\Tests\Decoded;
use Drupdater\Agent\Tests\Fails;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;

/**
 * The section goldens are the "pull request section" examples on the addon reference pages.
 */
final class MergeRequestTest extends TestCase
{
    use Fails;

    private const INTRO = 'This automated merge request by [Drupdater](https://github.com/drupdater/drupdater) includes '
        . 'updates for your Drupal site. Please review the changes carefully to ensure compatibility and stability '
        . "before merging.\n";

    public function testTheDescriptionOfThePublishedExampleIsMadeOfItsSections(): void
    {
        $addons = Decoded::object(self::publishedExample()->addons);

        $description = MergeRequest::builtin()->description($addons);

        self::assertSame(self::golden('description'), $description);
        foreach (MergeRequest::builtin()->sections($addons) as $name => $section) {
            self::assertSame(self::golden($name), $section, $name);
            self::assertStringContainsString($section, $description, $name);
        }
    }

    public function testThePublishedExampleReachesEverySectionInOrder(): void
    {
        self::assertSame([
            'composer_allow_plugins',
            'composer_patches',
            'composer_diff',
            'composer_audit',
            'unsupported_modules',
            'update_hooks',
        ], array_keys(MergeRequest::builtin()->sections(Decoded::object(self::publishedExample()->addons))));
    }

    public function testTheTitleNamesTheModeAndTheStart(): void
    {
        self::assertSame('July 2026: Drupal Maintenance Updates', MergeRequest::title(self::report(['mode' => 'normal', 'started_at' => '2026-07-25T09:00:00Z'])));
        self::assertSame('2026-07-25: Drupal Security Updates', MergeRequest::title(self::report(['mode' => 'security', 'started_at' => '2026-07-25T09:00:00Z'])));
    }

    public function testTheTitleDatesTheStartInTheZoneTheReportGivesIt(): void
    {
        self::assertSame('2026-08-01: Drupal Security Updates', MergeRequest::title(self::report(['mode' => 'security', 'started_at' => '2026-08-01T00:30:00.123456789+02:00'])));
        self::assertSame('August 2026: Drupal Maintenance Updates', MergeRequest::title(self::report(['mode' => 'normal', 'started_at' => '2026-08-01T00:30:00.5+02:00'])));
    }

    /**
     * @return iterable<string, array{mixed}>
     */
    public static function unreadableStarts(): iterable
    {
        yield 'missing' => [null];
        yield 'not a string' => [1];
        yield 'no zone' => ['2026-07-25T09:00:00'];
        yield 'a date alone' => ['2026-07-25'];
    }

    #[DataProvider('unreadableStarts')]
    public function testATitleNeedsAnRfc3339Start(mixed $started): void
    {
        self::assertFails(Failure::INVALID_REQUEST, 'the report\'s started_at must be an RFC 3339 time', static fn () => MergeRequest::title(self::report(['started_at' => $started])));
    }

    public function testAReportWithoutSectionsDescribesOnlyTheMergeRequest(): void
    {
        self::assertSame(self::INTRO, self::describe([]));
    }

    /**
     * @return iterable<string, array{array<string, mixed>, string}>
     */
    public static function unreadableSections(): iterable
    {
        yield 'audit that is no object' => [['composer_audit' => 'not an object'], 'the report\'s composer_audit section is unreadable: the section must be an object'];
        yield 'advisories that are no list' => [['composer_audit' => ['fixed' => 'x']], 'the report\'s composer_audit section is unreadable: fixed must be a list'];
        yield 'an advisory that is no object' => [['composer_audit' => ['fixed' => ['x']]], 'the report\'s composer_audit section is unreadable: fixed must hold objects'];
        yield 'a title that is no string' => [['composer_audit' => ['fixed' => [['title' => 1]]]], 'the report\'s composer_audit section is unreadable: title must be a string'];
        yield 'plugins that are no strings' => [['composer_allow_plugins' => ['added' => [1]]], 'the report\'s composer_allow_plugins section is unreadable: added must hold strings'];
        yield 'dev that is no boolean' => [['composer_diff' => [['package' => 'a', 'dev' => 'yes']]], 'the report\'s composer_diff section is unreadable: dev must be a boolean'];
        yield 'a site that is no object' => [['update_hooks' => ['default' => []]], 'the report\'s update_hooks section is unreadable: site default must be an object'];
        yield 'a hook that is no object' => [['update_hooks' => ['default' => ['token_update_8002' => 'Rebuild']]], 'the report\'s update_hooks section is unreadable: hook token_update_8002 must be an object'];
    }

    /**
     * @param array<string, mixed> $addons
     */
    #[DataProvider('unreadableSections')]
    public function testAnUnreadableSectionFailsTheDescription(array $addons, string $reason): void
    {
        self::assertFails(Failure::INVALID_REQUEST, $reason, static fn () => self::describe($addons));
    }

    public function testAPluginListRendersOnlyWhenSomethingWasAdded(): void
    {
        self::assertStringNotContainsString('New Composer plugins', self::describe(['composer_allow_plugins' => ['added' => []]]));

        $one = self::describe(['composer_allow_plugins' => ['added' => ['acme/plugin']]]);
        self::assertStringContainsString('## 🔌 New Composer plugins', $one);
        self::assertStringContainsString("composer config allow-plugins.acme/plugin true\n", $one);
    }

    public function testASingleSiteListsItsHooksWithoutASiteHeading(): void
    {
        $description = self::describe(['update_hooks' => ['default' => ['token_update_8002' => ['description' => 'Rebuild']]]]);

        self::assertStringContainsString("| token_update_8002 | Rebuild |\n", $description);
        self::assertStringNotContainsString('### Site:', $description);
    }

    public function testHooksAndSitesAreListedByNameWhateverTheReportsOrder(): void
    {
        $description = self::describe(['update_hooks' => [
            'site2' => ['b_update' => ['description' => 'B'], '9001' => ['description' => 'Numeric'], 'a_update' => ['description' => 'A']],
            'site10' => ['x' => ['description' => 'X']],
        ]]);

        self::assertStringContainsString("### Site: site10\n\n| Hook | Description |\n| ---- | ----------- |\n| x | X |\n\n### Site: site2\n", $description);
        self::assertStringContainsString("| 9001 | Numeric |\n| a_update | A |\n| b_update | B |\n\n</details>\n", $description);
    }

    public function testAnEmptyUpdateHooksSectionRendersNothing(): void
    {
        self::assertSame(self::INTRO, self::describe(['update_hooks' => new \stdClass()]));
    }

    public function testSectionsWithoutARendererAreIgnored(): void
    {
        self::assertStringNotContainsString('##', self::describe(['code_beautifier' => ['files' => ['a.php'], 'fixable' => 1]]));
    }

    public function testAdvisoryCellsCannotBreakTheTable(): void
    {
        $description = self::describe(['composer_audit' => [
            'remaining' => [['packageName' => 'drupal/foo', 'cve' => 'CVE-1', 'title' => "XSS via a|b\nsecond\r line"]],
        ]]);

        self::assertStringContainsString('| ⛔     | CVE-1 | XSS via a\\|b second line |  | drupal/foo |', $description);
        self::assertStringNotContainsString('a|b', $description);
        self::assertStringContainsString('There are still 1 unresolved issue. Please investigate manually.', $description);
    }

    public function testSeveralRemainingAdvisoriesArePlural(): void
    {
        $description = self::describe(['composer_audit' => ['remaining' => [['cve' => 'CVE-1'], ['cve' => 'CVE-2']]]]);

        self::assertStringContainsString('There are still 2 unresolved issues. Please investigate manually.', $description);
    }

    public function testASecurityRunThatFixedEverythingSaysSo(): void
    {
        $description = self::describe(['composer_audit' => ['fixed' => [['packageName' => 'drupal/core', 'cve' => 'CVE-1', 'title' => 'Closed']]]]);

        self::assertStringContainsString('| ✅     | CVE-1 | Closed |', $description);
        self::assertStringContainsString('All security issues have been resolved.', $description);
    }

    public function testAbandonedPackagesAloneGetTheEndOfLifeTableNotTheSecuritySection(): void
    {
        $description = self::describe(['composer_audit' => ['abandoned' => [['packageName' => 'patchwork/jsqueeze', 'replacement' => '']]]]);

        self::assertStringNotContainsString('Security Report', $description);
        self::assertStringContainsString('| patchwork/jsqueeze | Abandoned package | — | No replacement suggested |', $description);
    }

    public function testTheEndOfLifeTableJoinsBothSourcesByName(): void
    {
        $description = self::describe([
            'unsupported_modules' => [
                ['name' => 'upgrade', 'installed_version' => '2.0.0', 'recommended_version' => '3.0.0'],
                ['name' => 'none', 'installed_version' => '1.0.0', 'recommended_version' => 'None'],
                ['name' => 'empty'],
            ],
            'composer_audit' => ['abandoned' => [
                ['packageName' => 'b/orphan', 'replacement' => ''],
                ['packageName' => 'a/replaced', 'replacement' => 'a/next'],
            ]],
        ]);

        self::assertStringContainsString("| Name | Status | Installed version | Recommendation |\n"
            . "| ---- | ------ | ----------------- | -------------- |\n"
            . "| a/replaced | Abandoned package | — | Replace with a/next |\n"
            . "| b/orphan | Abandoned package | — | No replacement suggested |\n"
            . "| empty | Unsupported module | — | No supported release — replace it |\n"
            . "| none | Unsupported module | 1.0.0 | No supported release — replace it |\n"
            . "| upgrade | Unsupported module | 2.0.0 | Update to 3.0.0 |\n", $description);
    }

    public function testEndOfLifeCellsCannotBreakTheTable(): void
    {
        $description = self::describe(['unsupported_modules' => [['name' => 'a|b', 'installed_version' => "1\n0", 'recommended_version' => '2|0']]]);

        self::assertStringContainsString('| a\\|b | Unsupported module | 1 0 | Update to 2\\|0 |', $description);
    }

    /**
     * @return iterable<string, array{array<string, mixed>}>
     */
    public static function patchChanges(): iterable
    {
        yield 'only removals' => [['removed' => [['package' => 'drupal/core', 'patch_path' => 'p.patch', 'reason' => 'fixed']], 'updated' => [], 'conflicts' => []]];
        yield 'only updates' => [['removed' => [], 'updated' => [['package' => 'drupal/core', 'previous_patch_path' => 'old.patch', 'new_patch_path' => 'new.patch']], 'conflicts' => []]];
        yield 'only conflicts' => [['removed' => [], 'updated' => [], 'conflicts' => [['package' => 'drupal/core', 'fixed_version' => '1.0', 'new_version' => '1.1']]]];
    }

    /**
     * @param array<string, mixed> $patches
     */
    #[DataProvider('patchChanges')]
    public function testAnyPatchChangeAloneRendersThePatchSection(array $patches): void
    {
        $sections = self::sections(['composer_patches' => $patches]);

        self::assertSame(['composer_patches'], array_keys($sections));
        self::assertStringStartsWith("## 🩹 Patch updates\n\n", $sections['composer_patches']);
        self::assertStringContainsString('drupal/core', $sections['composer_patches']);
        self::assertSame(2, substr_count($sections['composer_patches'], '**'), 'one bold heading of the three');
    }

    public function testAPatchSectionWithNothingInItStillSaysSo(): void
    {
        self::assertSame(['composer_patches' => "## 🩹 Patch updates\n"], self::sections(['composer_patches' => new \stdClass()]));
    }

    public function testDependencyTablesSplitProdFromDev(): void
    {
        $description = self::describe(['composer_diff' => [
            ['package' => 'drupal/core', 'operation' => 'change', 'from' => 'dev-main abc', 'to' => 'dev-main def', 'dev' => false],
            ['package' => 'acme/tool', 'operation' => 'downgrade', 'from' => '2.0', 'to' => '1.9', 'dev' => true],
            ['package' => 'acme/odd', 'operation' => 'renamed|', 'from' => '1.0', 'to' => '1.1'],
        ]]);

        self::assertStringContainsString("| Prod Packages | Operation | Base | Target | Link |\n", $description);
        self::assertStringContainsString("| drupal/core | Changed | dev-main abc | dev-main def |  |\n", $description);
        self::assertStringContainsString("| acme/odd | renamed\\| | 1.0 | 1.1 |  |\n", $description, 'an unknown operation is shown as is');
        self::assertStringContainsString("| Dev Packages | Operation | Base | Target | Link |\n| --- | --- | --- | --- | --- |\n| acme/tool | Downgraded | 2.0 | 1.9 |  |\n", $description);
        self::assertLessThan(strpos($description, 'Dev Packages'), strpos($description, 'Prod Packages'));
    }

    public function testOnlyDevChangesRenderOnlyTheDevTable(): void
    {
        $description = self::describe(['composer_diff' => [['package' => 'acme/tool', 'operation' => 'install', 'to' => '1.0', 'dev' => true]]]);

        self::assertStringNotContainsString('Prod Packages', $description);
        self::assertStringContainsString("| acme/tool | New | - | 1.0 |  |\n", $description);
    }

    public function testEveryOperationHasItsLabel(): void
    {
        $description = self::describe(['composer_diff' => array_map(
            static fn (string $operation): array => ['package' => $operation, 'operation' => $operation, 'from' => '1', 'to' => '2'],
            ['install', 'remove', 'upgrade', 'downgrade', 'change'],
        )]);

        foreach (['install' => 'New', 'remove' => 'Removed', 'upgrade' => 'Upgraded', 'downgrade' => 'Downgraded', 'change' => 'Changed'] as $operation => $label) {
            self::assertStringContainsString("| {$operation} | {$label} | 1 | 2 |  |\n", $description);
        }
    }

    public function testAnOversizedDescriptionDropsTheDependencyLinks(): void
    {
        $url = 'https://github.com/acme/' . str_repeat('x', 300);
        $changes = [];
        for ($i = 0; $i < 300; $i++) {
            $changes[] = ['package' => 'acme/p' . str_repeat('y', $i % 10), 'operation' => 'upgrade', 'from' => '1.0', 'to' => '1.1', 'compare_url' => $url, 'project_url' => $url];
        }

        $description = self::describe(['composer_diff' => $changes]);

        self::assertLessThanOrEqual(MergeRequest::MAX_DESCRIPTION_BYTES, strlen($description));
        self::assertStringNotContainsString('[Compare]', $description);
        self::assertStringNotContainsString('| Link |', $description);
        self::assertStringContainsString("| --- | --- | --- | --- |\n| acme/p | Upgraded | 1.0 | 1.1 |\n", $description);
    }

    public function testADescriptionAtExactlyTheLimitKeepsItsLinks(): void
    {
        $withName = static fn (string $name): array => ['composer_diff' => [['package' => $name, 'operation' => 'upgrade', 'from' => '1.0', 'to' => '1.1', 'compare_url' => 'https://c']]];
        $base = self::describe($withName('a'));

        $description = self::describe($withName(str_repeat('a', 1 + MergeRequest::MAX_DESCRIPTION_BYTES - strlen($base))));

        self::assertSame(MergeRequest::MAX_DESCRIPTION_BYTES, strlen($description));
        self::assertStringContainsString('[Compare](https://c)', $description);
    }

    public function testADescriptionWithinTheLimitKeepsItsLinks(): void
    {
        $description = self::describe(['composer_diff' => [
            ['package' => 'drupal/core', 'operation' => 'upgrade', 'from' => '1.0', 'to' => '1.1', 'compare_url' => 'https://c', 'project_url' => 'https://p'],
        ]]);

        self::assertStringContainsString("| [drupal/core](https://p) | Upgraded | 1.0 | 1.1 | [Compare](https://c) |\n", $description);
    }

    private static function publishedExample(): \stdClass
    {
        $request = json_decode((string) file_get_contents(__DIR__ . '/../../protocol/merge-request/request.json'), false, 512, JSON_THROW_ON_ERROR);
        return Decoded::object(Json::at($request, 'request', 'report'));
    }

    private static function golden(string $name): string
    {
        return (string) file_get_contents(__DIR__ . '/testdata/' . $name . '.md');
    }

    /**
     * @param array<string, mixed> $report
     */
    private static function report(array $report): \stdClass
    {
        return Decoded::object(json_decode(json_encode($report, JSON_THROW_ON_ERROR), false, 512, JSON_THROW_ON_ERROR));
    }

    /**
     * @param array<string, mixed> $addons
     */
    private static function describe(array $addons): string
    {
        return MergeRequest::builtin()->description(self::addons($addons));
    }

    /**
     * @param array<string, mixed> $addons
     * @return array<string, string>
     */
    private static function sections(array $addons): array
    {
        return MergeRequest::builtin()->sections(self::addons($addons));
    }

    /**
     * @param array<string, mixed> $addons
     */
    private static function addons(array $addons): \stdClass
    {
        return $addons === [] ? new \stdClass() : self::report($addons);
    }
}
