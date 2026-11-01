<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\Addon;

use Drupdater\Agent\Addon\CodeBeautifier;
use Drupdater\Agent\Addon\Commit;
use Drupdater\Agent\Addon\DeprecationsRemover;
use Drupdater\Agent\Addon\Event;
use Drupdater\Agent\Addon\HookResult;
use Drupdater\Agent\Failure;
use Drupdater\Agent\Log;
use Drupdater\Agent\Project\Project;
use Drupdater\Agent\Tests\Scratch;
use Drupdater\Agent\Tools\Phpcs;
use Drupdater\Agent\Tools\Rector;
use PHPUnit\Framework\TestCase;

/**
 * code_beautifier and deprecations_remover, the two addons that rewrite the custom code.
 */
final class CodeQualityAddonsTest extends TestCase
{
    use Scratch;

    private const RULESET = '<?xml version="1.0"?><ruleset name="p"><file>web/modules/custom</file><rule ref="Drupal"/></ruleset>';

    public function testWritesAndCommitsAPhpcsConfigOverTheCustomCodeWhereThereIsNone(): void
    {
        $project = $this->scratchDir();
        $phpcs = $this->phpcsReporting(['files' => [], 'totals' => ['errors' => 0, 'warnings' => 0, 'fixable' => 0]]);

        $result = $this->beautifier(new Project(customCodeDirectories: ['web/modules/custom'], packages: ['drupal/core' => '10.3.9']), bundled: $phpcs)
            ->handle(Event::postCodeUpdate($project));

        self::assertSame([['message' => 'Add PHPCS config', 'paths' => ['phpcs.xml']]], self::commits($result));
        self::assertSame(CodeBeautifier::config(['web/modules/custom'], '10'), file_get_contents($project . '/phpcs.xml'));
        self::assertStringContainsString('<config name="drupal_core_version" value="10"/>', (string) file_get_contents($project . '/phpcs.xml'));
    }

    public function testSkipsAProjectWithNeitherAConfigNorCustomCode(): void
    {
        $project = $this->scratchDir();

        $result = $this->beautifier(new Project())->handle(Event::postCodeUpdate($project));

        self::assertSame([], $result->commits);
        self::assertFileDoesNotExist($project . '/phpcs.xml');
    }

    public function testSkipsAConfigThatCoversNoFiles(): void
    {
        $project = $this->scratchDir();
        $this->writeFiles($project, ['phpcs.xml' => '<?xml version="1.0"?><ruleset name="p"><rule ref="Drupal"/></ruleset>']);
        $log = new Log();

        $result = (new CodeBeautifier($log, static fn (): Project => new Project(), $this->unused(), $this->unused()))->handle(Event::postCodeUpdate($project));

        self::assertSame([], $result->commits);
        self::assertContains(['level' => 'warning', 'message' => 'phpcs.xml found but no file path definitions, skipping coding style update', 'context' => []], $log->entries());
    }

    public function testReadsAPhpcsXmlDistToo(): void
    {
        $project = $this->scratchDir();
        $this->writeFiles($project, ['phpcs.xml.dist' => self::RULESET]);

        $result = $this->beautifier(new Project(), bundled: $this->phpcsReporting(['files' => [], 'totals' => ['errors' => 0, 'warnings' => 0, 'fixable' => 0]]))->handle(Event::postCodeUpdate($project));

        self::assertSame([], $result->commits, 'it ran phpcs, which found nothing to fix');
    }

    public function testAConfigThatIsNoRulesetFailsTheHook(): void
    {
        foreach (['not XML' => '<ruleset', 'another root' => '<?xml version="1.0"?><phpunit/>'] as $config) {
            $project = $this->scratchDir();
            $this->writeFiles($project, ['phpcs.xml' => $config]);
            try {
                $this->beautifier(new Project())->handle(Event::postCodeUpdate($project));
                self::fail('a malformed config was accepted');
            }
            catch (Failure $failure) {
                self::assertSame('failed to parse phpcs config', $failure->reason);
            }
        }
    }

    public function testAConfigThatCannotBeReadFailsTheHook(): void
    {
        $project = $this->scratchDir();
        mkdir($project . '/phpcs.xml');

        $this->expectExceptionObject(new Failure(Failure::FAILED, 'failed to read phpcs config', $project . '/phpcs.xml'));
        $this->beautifier(new Project())->handle(Event::postCodeUpdate($project));
    }

    public function testAConfigThatCannotBeWrittenFailsTheHook(): void
    {
        $project = $this->scratchDir() . '/gone';

        $this->expectExceptionObject(new Failure(Failure::FAILED, 'failed to write phpcs.xml', $project . '/phpcs.xml'));
        $this->beautifier(new Project(customCodeDirectories: ['web/modules/custom']))->handle(Event::postCodeUpdate($project));
    }

    public function testRunsOurPhpcsWhenTheProjectCannotBeRead(): void
    {
        $project = $this->scratchDir();
        $this->writeFiles($project, ['phpcs.xml' => self::RULESET]);
        $unreadable = static fn (): Project => throw new Failure(Failure::INTERNAL, 'composer.json could not be read');

        (new CodeBeautifier(new Log(), $unreadable, $this->unused(), $this->phpcsReporting(['files' => [], 'totals' => ['errors' => 0, 'warnings' => 0, 'fixable' => 0]])))
            ->handle(Event::postCodeUpdate($project));
    }

    public function testRunsTheProjectsOwnPhpcsWhereItHasCoderAndOursWhereNot(): void
    {
        $clean = ['files' => [], 'totals' => ['errors' => 0, 'warnings' => 0, 'fixable' => 0]];
        $project = $this->scratchDir();
        $this->writeFiles($project, ['phpcs.xml' => self::RULESET]);

        $this->beautifier(new Project(packages: ['drupal/coder' => '8.3.0']), projectPhpcs: $this->phpcsReporting($clean), bundled: $this->unused())->handle(Event::postCodeUpdate($project));
        $this->beautifier(new Project(), projectPhpcs: $this->unused(), bundled: $this->phpcsReporting($clean))->handle(Event::postCodeUpdate($project));
    }

    public function testFixesAndCommitsTheFilesWithViolationsRelativeAndSorted(): void
    {
        $project = $this->scratchDir();
        $this->writeFiles($project, ['phpcs.xml' => self::RULESET]);
        $phpcs = $this->createMock(Phpcs::class);
        $phpcs->method('report')->willReturn(['files' => [
            $project . '/web/modules/custom/b.php' => ['errors' => 1, 'warnings' => 0],
            $project . '/web/modules/custom/clean.php' => ['errors' => 0, 'warnings' => 0],
            'web/modules/custom/a.php' => ['errors' => 0, 'warnings' => 2],
        ], 'totals' => ['errors' => 1, 'warnings' => 2, 'fixable' => 3]]);
        $phpcs->expects(self::once())->method('fix');

        $result = $this->beautifier(new Project(), bundled: $phpcs)->handle(Event::postCodeUpdate($project));

        $files = ['web/modules/custom/a.php', 'web/modules/custom/b.php'];
        self::assertSame([['message' => 'Update coding styles', 'paths' => $files]], self::commits($result));
        self::assertSame(['files' => $files, 'fixable' => 3], $result->finding);
    }

    public function testAPhpcbfThatLeavesViolationsStillCommitsWhatItFixed(): void
    {
        $project = $this->scratchDir();
        $this->writeFiles($project, ['phpcs.xml' => self::RULESET]);
        $phpcs = $this->phpcsReporting(['files' => ['a.php' => ['errors' => 1, 'warnings' => 0]], 'totals' => ['errors' => 1, 'warnings' => 0, 'fixable' => 1]]);
        $phpcs->method('fix')->willThrowException(new Failure(Failure::FAILED, 'phpcbf failed', 'exit code 1'));

        $result = $this->beautifier(new Project(), bundled: $phpcs)->handle(Event::postCodeUpdate($project));

        self::assertSame([['message' => 'Update coding styles', 'paths' => ['a.php']]], self::commits($result));
    }

    public function testTheConfigItWroteIsCommittedBeforeTheFixes(): void
    {
        $project = $this->scratchDir();
        $phpcs = $this->phpcsReporting(['files' => ['a.php' => ['errors' => 1, 'warnings' => 0]], 'totals' => ['errors' => 1, 'warnings' => 0, 'fixable' => 1]]);

        $result = $this->beautifier(new Project(customCodeDirectories: ['web/modules/custom']), bundled: $phpcs)->handle(Event::postCodeUpdate($project));

        self::assertSame(['Add PHPCS config', 'Update coding styles'], array_column(self::commits($result), 'message'));
    }

    public function testTheBeautifierReportsTheFixesOnlyOnceTheirCommitWasMade(): void
    {
        $fixes = ['files' => ['a.php'], 'fixable' => 1];

        self::assertSame($fixes, CodeBeautifier::report(Records::findings([Records::record('post-code-update', $fixes, committed: ['Add PHPCS config', 'Update coding styles'])])));
        self::assertNull(CodeBeautifier::report(Records::findings([Records::record('post-code-update', $fixes, committed: ['Add PHPCS config'])])), 'phpcbf changed nothing');
        self::assertNull(CodeBeautifier::report(Records::findings()));
    }

    public function testRemovesDeprecationsWithTheProjectsRectorWhereItHasDrupalRector(): void
    {
        $rector = $this->createMock(Rector::class);
        $rector->expects(self::once())->method('process')->with(['web/modules/custom'])->willReturn([
            'changed_files' => ['web/modules/custom/b.php', 'web/modules/custom/a.php'],
            'file_diffs' => [
                ['file' => 'web/modules/custom/b.php', 'applied_rectors' => ['Z\\Rule', 'A\\Rule']],
                ['file' => 'web/modules/custom/a.php', 'applied_rectors' => []],
            ],
        ]);

        $result = $this->remover(new Project(customCodeDirectories: ['web/modules/custom'], packages: ['palantirnet/drupal-rector' => '0.20.0']), own: $rector)
            ->handle(Event::postCodeUpdate('/p'));

        self::assertSame([['message' => 'Remove deprecations', 'paths' => ['web/modules/custom/b.php', 'web/modules/custom/a.php']]], self::commits($result));
        self::assertSame([
            ['file' => 'web/modules/custom/a.php'],
            ['file' => 'web/modules/custom/b.php', 'applied_rectors' => ['A\\Rule', 'Z\\Rule']],
        ], $result->finding);
    }

    public function testRunsOurRectorWhereTheProjectHasNone(): void
    {
        $rector = $this->createMock(Rector::class);
        $rector->expects(self::once())->method('process')->willReturn(['changed_files' => [], 'file_diffs' => []]);

        $result = $this->remover(new Project(customCodeDirectories: ['web/modules/custom']), bundled: $rector)->handle(Event::postCodeUpdate('/p'));

        self::assertSame([], $result->commits);
        self::assertNull($result->finding);
    }

    public function testSkipsAProjectWithARectorThatIsNotDrupalRector(): void
    {
        $log = new Log();

        $result = (new DeprecationsRemover($log, static fn (): Project => new Project(packages: ['rector/rector' => '2.0.0']), self::noRector(...), self::noRector(...)))
            ->handle(Event::postCodeUpdate('/p'));

        self::assertSame([], $result->commits);
        self::assertContains(['level' => 'warning', 'message' => 'skipping deprecation removal: the project requires rector/rector but not palantirnet/drupal-rector', 'context' => []], $log->entries());
    }

    public function testTheRemoverReportsItsFixes(): void
    {
        $fixes = [['file' => 'a.php', 'applied_rectors' => ['R']]];

        self::assertSame($fixes, DeprecationsRemover::report(Records::findings([Records::record('post-code-update', $fixes)])));
        self::assertNull(DeprecationsRemover::report(Records::findings([Records::record('post-code-update', null)])));
    }

    /**
     * @param array<string, mixed> $report
     */
    private function phpcsReporting(array $report): Phpcs&\PHPUnit\Framework\MockObject\MockObject
    {
        $phpcs = $this->createMock(Phpcs::class);
        $phpcs->method('report')->willReturn($report);
        return $phpcs;
    }

    private function unused(): Phpcs
    {
        $phpcs = $this->createMock(Phpcs::class);
        $phpcs->expects(self::never())->method('report');
        return $phpcs;
    }

    private function beautifier(Project $project, ?Phpcs $projectPhpcs = null, ?Phpcs $bundled = null): CodeBeautifier
    {
        return new CodeBeautifier(new Log(), static fn (): Project => $project, $projectPhpcs ?? $this->unused(), $bundled ?? $this->unused());
    }

    private function remover(Project $project, ?Rector $own = null, ?Rector $bundled = null): DeprecationsRemover
    {
        return new DeprecationsRemover(
            new Log(),
            static fn (): Project => $project,
            static fn (): Rector => $own ?? self::noRector(),
            static fn (): Rector => $bundled ?? self::noRector(),
        );
    }

    private static function noRector(): Rector
    {
        throw new \LogicException('no Rector should have been built');
    }

    /**
     * @return list<array{message: string, paths: list<string>}>
     */
    private static function commits(HookResult $result): array
    {
        return array_map(static fn (Commit $commit): array => $commit->toArray(), $result->commits);
    }
}
