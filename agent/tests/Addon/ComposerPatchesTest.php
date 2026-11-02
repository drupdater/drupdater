<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\Addon;

use Drupdater\Agent\Addon\Commit;
use Drupdater\Agent\Addon\ComposerPatches;
use Drupdater\Agent\Addon\Event;
use Drupdater\Agent\Addon\HookResult;
use Drupdater\Agent\Addon\Pin;
use Drupdater\Agent\Addon\Plan;
use Drupdater\Agent\DrupalOrg\DrupalCode;
use Drupdater\Agent\DrupalOrg\DrupalOrg;
use Drupdater\Agent\DrupalOrg\ForkMergeRequest;
use Drupdater\Agent\DrupalOrg\Issue;
use Drupdater\Agent\Failure;
use Drupdater\Agent\Log;
use Drupdater\Agent\PatchCheck\PatchChecker;
use Drupdater\Agent\Patches\PatchDownloader;
use Drupdater\Agent\Patches\PatchWriter;
use Drupdater\Agent\Project\Project;
use Drupdater\Agent\Resolve\Resolver;
use Drupdater\Agent\Tests\Scratch;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;

final class ComposerPatchesTest extends TestCase
{
    use Scratch;

    private const ISSUE_PATCH = 'https://www.drupal.org/files/issues/2024-01-01/3123456-12.patch';
    private const MERGE_REQUEST = 'https://git.drupalcode.org/project/token/-/merge_requests/7';

    private string $project = '';
    private Log $log;
    /** @var array<string, string> */
    private array $installed = ['drupal/token' => '1.14.0', 'drupal/core' => '10.3.8'];
    /** @var list<array{action: string, package: string, from: string, to: string}> */
    private array $operations = [];
    /** @var list<array{string, string, string, list<string>}> */
    private array $checked = [];
    /** @var array<string, bool|\Exception> by the last patch checked */
    private array $verdicts = [];
    private ?\stdClass $written = null;
    private ?DrupalCode $drupalCode = null;
    private ?Issue $issue = null;
    /** @var array<string, array<string, string>> */
    private array $dependencyPatches = [];
    private ?Plan $planResolved = null;

    protected function setUp(): void
    {
        $this->project = $this->scratchDir();
        $this->log = new Log();
    }

    public function testRemovesThePatchesOfAPackageNoLongerInstalledAndCommitsTheirFiles(): void
    {
        $this->writeFiles($this->project, ['patches/gone.patch' => 'x']);
        unset($this->installed['drupal/token']);

        $result = $this->run_(['drupal/token' => ['Local fix' => 'patches/gone.patch', 'Remote' => 'https://example.com/r.patch']]);

        self::assertSame([
            ['package' => 'drupal/token', 'patch_description' => 'Local fix', 'patch_path' => 'patches/gone.patch', 'reason' => 'drupal/token is not installed in the project'],
            ['package' => 'drupal/token', 'patch_description' => 'Remote', 'patch_path' => 'https://example.com/r.patch', 'reason' => 'drupal/token is not installed in the project'],
        ], $result->finding['removed']);
        self::assertEquals(new \stdClass(), $this->written);
        self::assertFileDoesNotExist($this->project . '/patches/gone.patch');
        self::assertSame([['message' => 'Update patches', 'paths' => ['composer.json', 'composer.lock', 'patches/gone.patch']]], self::commits($result));
    }

    public function testAnEmptyPatchSetOfAnUninstalledPackageIsDroppedWithTheRestButReportsNothing(): void
    {
        unset($this->installed['drupal/token']);

        $result = $this->run_(['drupal/token' => ['Remote' => 'https://example.com/r.patch'], 'drupal/empty' => new \stdClass()]);

        self::assertSame(['drupal/token'], array_column($result->finding['removed'], 'package'));
        self::assertEquals(new \stdClass(), $this->written);
    }

    public function testHoldsBackAPackageWhosePatchNoLongerApplies(): void
    {
        $this->operations = [self::upgrade('drupal/token', '1.14.0', '1.15.0')];
        $this->verdicts = [$this->project . '/patches/fix.patch' => false];

        $result = $this->run_(['drupal/token' => ['Local fix' => 'patches/fix.patch']]);

        self::assertSame([[
            'package' => 'drupal/token', 'fixed_version' => '1.14.0', 'patch_path' => 'patches/fix.patch', 'patch_description' => 'Local fix', 'new_version' => '1.15.0',
        ]], $result->finding['conflicts']);
        self::assertEquals([new Pin('drupal/token', '1.14.0')], $result->plan?->keep);
        self::assertNotNull($this->written, 'a conflict is a change, so the patches are written back as they were');
    }

    public function testChangesNothingWhenEveryPatchStillApplies(): void
    {
        $this->operations = [self::upgrade('drupal/token', '1.14.0', '1.15.0')];

        $result = $this->run_(['drupal/token' => ['Local fix' => 'patches/fix.patch']]);

        self::assertSame(['removed' => [], 'updated' => [], 'conflicts' => []], $result->finding);
        self::assertSame([], $result->commits);
        self::assertSame([], $result->plan?->keep);
        self::assertNull($this->written);
        self::assertSame([['drupal/token', '1.15.0', '', [$this->project . '/patches/fix.patch']]], $this->checked);
    }

    public function testAProjectWithoutPatchesChangesNothing(): void
    {
        $this->operations = [self::upgrade('drupal/token', '1.14.0', '1.15.0')];

        $result = $this->run_(null);

        self::assertSame([], $result->commits);
        self::assertSame([], $this->checked);
    }

    public function testPatchesThatAreNoObjectFailTheHook(): void
    {
        $this->expectExceptionObject(new Failure(Failure::FAILED, 'extra.patches is not an object'));
        $this->addon(['a.patch'])->handle(Event::preComposerUpdate($this->project, new Plan()));
    }

    public function testDryRunsTheUpdateAsEarlierStagesPlannedIt(): void
    {
        $plan = new Plan(['drupal/token'], [new Pin('drupal/core', '10.3.8')], true);

        $this->addon(new \stdClass())->handle(Event::preComposerUpdate($this->project, $plan));

        self::assertEquals($plan, $this->planResolved);
    }

    public function testAFailedDryRunFailsTheHook(): void
    {
        $resolver = $this->createMock(Resolver::class);
        $resolver->method('resolve')->willThrowException(new Failure(Failure::FAILED, 'composer update failed'));

        $this->expectExceptionObject(new Failure(Failure::FAILED, 'composer update failed'));
        $this->addon((object) [], $resolver)->handle(Event::preComposerUpdate($this->project, new Plan()));
    }

    public function testChecksAPackagesPatchesTogetherInDeclarationOrder(): void
    {
        $this->operations = [self::upgrade('drupal/token', '1.14.0', '1.15.0')];

        $this->run_(['drupal/token' => ['Second' => 'patches/b.patch', 'First' => 'patches/a.patch']]);

        self::assertSame([$this->project . '/patches/b.patch', $this->project . '/patches/a.patch'], end($this->checked)[3]);
    }

    public function testHoldsBackAPackageWhosePatchesDoNotApplyTogether(): void
    {
        $this->operations = [self::upgrade('drupal/token', '1.14.0', '1.15.0')];
        $this->verdicts = [$this->project . '/patches/a.patch' . '|together' => false];

        $result = $this->run_(['drupal/token' => ['First' => 'patches/b.patch', 'Second' => 'patches/a.patch']]);

        self::assertSame([['package' => 'drupal/token', 'fixed_version' => '1.14.0', 'patch_path' => '', 'patch_description' => 'Multiple patches do not apply together', 'new_version' => '1.15.0']], $result->finding['conflicts']);
    }

    public function testAnUncheckablePatchLeavesThePackageUnpinned(): void
    {
        $this->operations = [self::upgrade('drupal/token', '1.14.0', '1.15.0')];
        $this->verdicts = [$this->project . '/patches/a.patch' => new Failure(Failure::UNOBTAINABLE, 'not available from any configured repository')];

        $result = $this->run_(['drupal/token' => ['Local' => 'patches/a.patch']]);

        self::assertSame([], $result->finding['conflicts']);
        self::assertSame([], $result->plan?->keep);
        self::assertContains([
            'level' => 'warning',
            'message' => 'could not check whether the patch still applies, leaving the package unpinned',
            'context' => ['package' => 'drupal/token', 'patch' => 'patches/a.patch', 'error' => 'not available from any configured repository'],
        ], $this->log->entries());
    }

    public function testAnUncheckableCombinationLeavesThePackageUnpinned(): void
    {
        $this->operations = [self::upgrade('drupal/token', '1.14.0', '1.15.0')];
        $this->verdicts = [$this->project . '/patches/b.patch|together' => new \RuntimeException('the network went away')];

        $result = $this->run_(['drupal/token' => ['First' => 'patches/a.patch', 'Second' => 'patches/b.patch']]);

        self::assertSame([], $result->finding['conflicts']);
    }

    public function testChecksABranchAtTheCommitItsVersionNames(): void
    {
        $this->operations = [self::upgrade('drupal/token', '1.x-dev 0424dff', '1.x-dev 9e1a2b3')];

        $this->run_(['drupal/token' => ['Local' => 'patches/a.patch']]);

        self::assertSame(['drupal/token', '1.x-dev', '9e1a2b3'], array_slice($this->checked[0], 0, 3));
    }

    public function testRemovesAPatchADependencyAlreadyAppliesButOnlyARemoteOne(): void
    {
        $this->dependencyPatches = ['drupal/token' => ['https://example.com/shared.patch', 'patches/local.patch']];

        $result = $this->run_(['drupal/token' => ['Shared' => 'https://example.com/shared.patch', 'Local' => 'patches/local.patch']]);

        self::assertSame([[
            'package' => 'drupal/token', 'patch_description' => 'Shared', 'patch_path' => 'https://example.com/shared.patch', 'reason' => 'Patch is already applied by a dependency of drupal/token',
        ]], $result->finding['removed']);
        self::assertEquals((object) ['drupal/token' => (object) ['Local' => 'patches/local.patch']], $this->written);
        self::assertSame([['message' => 'Update patches', 'paths' => ['composer.json', 'composer.lock']]], self::commits($result), 'a remote patch has no file to commit');
    }

    public function testRemovesThePatchesOfAPackageTheUpdateRemoves(): void
    {
        $this->writeFiles($this->project, ['patches/a.patch' => 'x']);
        $this->operations = [['action' => 'Remove', 'package' => 'drupal/token', 'from' => '1.14.0', 'to' => '']];

        $result = $this->run_(['drupal/token' => ['Local' => 'patches/a.patch']]);

        self::assertSame('drupal/token is no longer installed', $result->finding['removed'][0]['reason']);
        self::assertFileDoesNotExist($this->project . '/patches/a.patch');
    }

    public function testAPatchFileThatCannotBeRemovedIsLogged(): void
    {
        $this->operations = [['action' => 'Remove', 'package' => 'drupal/token', 'from' => '1.14.0', 'to' => '']];

        $this->run_(['drupal/token' => ['Local' => 'patches/missing.patch']]);

        self::assertContains(['level' => 'error', 'message' => 'failed to remove patch', 'context' => ['patch' => 'patches/missing.patch']], $this->log->entries());
    }

    /**
     * @return iterable<string, array{string, bool}>
     */
    public static function issueStatuses(): iterable
    {
        yield 'fixed' => ['2', true];
        yield 'closed (fixed)' => ['7', true];
        yield 'patch (to be ported)' => ['15', true];
        yield 'active' => ['1', false];
        yield 'needs review' => ['8', false];
    }

    #[DataProvider('issueStatuses')]
    public function testRemovesAPatchWhoseIssueIsFixedInTheNewVersion(string $status, bool $removed): void
    {
        $this->operations = [self::upgrade('drupal/token', '1.14.0', '1.15.0')];
        $this->issue = new Issue('3123456', 'Token fix', $status, 'https://www.drupal.org/node/3123456', 'token');
        $this->drupalCode = $this->drupalCode(fixed: true);

        $result = $this->run_(['drupal/token' => ['Token fix' => self::ISSUE_PATCH]]);

        self::assertSame($removed ? [[
            'package' => 'drupal/token',
            'patch_description' => 'Token fix',
            'patch_path' => self::ISSUE_PATCH,
            'reason' => 'Issue [#3123456](https://www.drupal.org/node/3123456) is fixed in drupal/token 1.15.0',
        ]] : [], $result->finding['removed']);
    }

    public function testAFixedIssuesPatchWhoseFileCannotBeRemovedStaysDeclared(): void
    {
        $this->operations = [self::upgrade('drupal/token', '1.14.0', '1.15.0')];
        $this->issue = new Issue('3123456', 'Token fix', '2', 'https://www.drupal.org/node/3123456', 'token');
        $this->drupalCode = $this->drupalCode(fixed: true);

        $result = $this->run_(['drupal/token' => ['Issue #3123456' => 'patches/missing.patch']]);

        self::assertSame([], $result->finding['removed'], 'it would vanish from composer.json unreported');
        self::assertContains(['level' => 'error', 'message' => 'failed to remove patch', 'context' => ['patch' => 'patches/missing.patch']], $this->log->entries());
    }

    public function testAFailedFixSearchIsLoggedAndThePatchCheckedAsIfUnfixed(): void
    {
        $this->operations = [self::upgrade('drupal/token', '1.14.0', '1.15.0')];
        $this->issue = new Issue('3123456', 'Token fix', '2', 'https://www.drupal.org/node/3123456', 'token');
        $this->drupalCode = $this->createMock(DrupalCode::class);
        $this->drupalCode->method('isFixedIn')->willThrowException(new \RuntimeException('search failed'));

        $result = $this->run_(['drupal/token' => ['Token fix' => self::ISSUE_PATCH]]);

        self::assertSame([], $result->finding['removed']);
        self::assertCount(1, $this->checked);
        self::assertContains(['level' => 'error', 'message' => 'failed to search commit history', 'context' => ['error' => 'search failed']], $this->log->entries());
    }

    public function testAMergeRequestThatCannotBeDownloadedChangesNothing(): void
    {
        $this->operations = [self::upgrade('drupal/token', '1.14.0', '1.15.0')];
        $this->issue = new Issue('3123456', 'Fix', '8', 'https://www.drupal.org/node/3123456', 'token');
        $this->drupalCode = $this->drupalCode(fixed: false);
        $this->drupalCode->method('issueForkMergeRequests')->willReturn([new ForkMergeRequest('abc123', self::MERGE_REQUEST)]);
        $this->verdicts = [$this->project . '/patches/old.patch' => false];
        $this->downloadFails = true;

        $result = $this->run_(['drupal/token' => ['Issue #3123456' => 'patches/old.patch']]);

        self::assertSame(['removed' => [], 'updated' => [], 'conflicts' => []], $result->finding);
    }

    public function testAReplacementWhoseOldFileCannotBeRemovedIsNotMadeAndLeavesNoDownloadBehind(): void
    {
        $this->operations = [self::upgrade('drupal/token', '1.14.0', '1.15.0')];
        $this->issue = new Issue('3123456', 'Fix', '8', 'https://www.drupal.org/node/3123456', 'token');
        $this->drupalCode = $this->drupalCode(fixed: false);
        $this->drupalCode->method('issueForkMergeRequests')->willReturn([new ForkMergeRequest('abc123', self::MERGE_REQUEST)]);
        $this->verdicts = [$this->project . '/patches/missing.patch' => false];

        $result = $this->run_(['drupal/token' => ['Issue #3123456' => 'patches/missing.patch']]);

        self::assertSame([], $result->finding['updated']);
        self::assertFileDoesNotExist($this->project . '/patches/token/3123456-abc123-fix.diff');
        self::assertContains(['level' => 'debug', 'message' => 'failed to remove old patch file', 'context' => ['patch' => 'patches/missing.patch']], $this->log->entries());
    }

    public function testAFixNotYetReleasedKeepsThePatchInItsPlaceUnderItsIssuesCanonicalDescription(): void
    {
        $this->operations = [self::upgrade('drupal/token', '1.14.0', '1.15.0')];
        $this->issue = new Issue('3123456', 'Token fix', '2', 'https://www.drupal.org/node/3123456', 'token');
        $this->verdicts = [self::ISSUE_PATCH => false];

        $result = $this->run_(['drupal/token' => ['Before' => 'patches/before.patch', 'old name' => self::ISSUE_PATCH, 'After' => 'patches/after.patch']]);

        self::assertSame(['Before', 'Issue #3123456: [Token fix](https://www.drupal.org/node/3123456)', 'After'], array_keys((array) $this->writtenOrProposed($result)->{'drupal/token'}));
    }

    public function testARenameThatWouldCollideKeepsTheOriginalDescription(): void
    {
        $this->operations = [self::upgrade('drupal/token', '1.14.0', '1.15.0')];
        $this->issue = new Issue('3123456', 'Token fix', '1', 'https://www.drupal.org/node/3123456', 'token');
        $canonical = 'Issue #3123456: [Token fix](https://www.drupal.org/node/3123456)';
        $this->verdicts = [self::ISSUE_PATCH => false];

        $result = $this->run_(['drupal/token' => [$canonical => 'patches/other.patch', 'old name' => self::ISSUE_PATCH]]);

        self::assertSame([$canonical, 'old name'], array_keys((array) $this->written?->{'drupal/token'}));
        self::assertSame('old name', $result->finding['conflicts'][0]['patch_description'] ?? null);
    }

    public function testAFixThatCannotBeConfirmedKeepsThePatchWithoutDrupalCode(): void
    {
        $this->operations = [self::upgrade('drupal/token', '1.14.0', '1.15.0')];
        $this->issue = new Issue('3123456', 'Token fix', '2', 'https://www.drupal.org/node/3123456', 'token');
        $this->verdicts = [self::ISSUE_PATCH => false];

        $result = $this->run_(['drupal/token' => ['Token fix' => self::ISSUE_PATCH]]);

        self::assertSame([], $result->finding['removed']);
        self::assertCount(1, $result->finding['conflicts']);
        self::assertContains('patch does not apply and no drupalcode client is configured, keeping current package version', array_column($this->log->entries(), 'message'));
    }

    public function testAnIssueThatCannotBeFetchedLeavesThePatchAlone(): void
    {
        $this->operations = [self::upgrade('drupal/token', '1.14.0', '1.15.0')];

        $result = $this->run_(['drupal/token' => ['Issue 3123456' => self::ISSUE_PATCH]], issueFails: true);

        self::assertSame(['removed' => [], 'updated' => [], 'conflicts' => []], $result->finding);
        self::assertSame([], $this->checked);
    }

    public function testReplacesAPatchThatNoLongerAppliesWithTheIssueForksMergeRequest(): void
    {
        $this->writeFiles($this->project, ['patches/token/old.patch' => 'x']);
        $this->operations = [self::upgrade('drupal/token', '1.14.0', '1.15.0')];
        $this->issue = new Issue('3123456', 'Token: fix it!', '8', 'https://www.drupal.org/node/3123456', 'token');
        $this->drupalCode = $this->drupalCode(fixed: false);
        $this->drupalCode->method('issueForkMergeRequests')->with('token', '3123456')->willReturn([new ForkMergeRequest('abc123', self::MERGE_REQUEST), new ForkMergeRequest('older', 'x')]);
        $this->verdicts = [$this->project . '/patches/token/old.patch' => false];

        $result = $this->run_(['drupal/token' => ['Issue #3123456' => 'patches/token/old.patch']]);

        $new = 'patches/token/3123456-abc123-token_fix_it.diff';
        $description = 'Issue #3123456: [Token: fix it!](https://www.drupal.org/node/3123456)';
        self::assertSame([['package' => 'drupal/token', 'patch_description' => $description, 'previous_patch_path' => 'patches/token/old.patch', 'new_patch_path' => $new]], $result->finding['updated']);
        self::assertSame(self::MERGE_REQUEST . '.diff', $this->downloaded[0]);
        self::assertFileExists($this->project . '/' . $new);
        self::assertFileDoesNotExist($this->project . '/patches/token/old.patch');
        self::assertEquals((object) [$description => $new], $this->written?->{'drupal/token'});
        self::assertSame([['message' => 'Update patches', 'paths' => ['composer.json', 'composer.lock', 'patches/token/old.patch', $new]]], self::commits($result));
    }

    public function testHoldsBackAPackageWhoseMergeRequestDoesNotApplyEitherAndLeavesNoDownloadBehind(): void
    {
        $this->operations = [self::upgrade('drupal/token', '1.14.0', '1.15.0')];
        $this->issue = new Issue('3123456', 'Fix', '8', 'https://www.drupal.org/node/3123456', 'token');
        $this->drupalCode = $this->drupalCode(fixed: false);
        $this->drupalCode->method('issueForkMergeRequests')->willReturn([new ForkMergeRequest('abc123', self::MERGE_REQUEST)]);
        $new = $this->project . '/patches/token/3123456-abc123-fix.diff';
        $this->verdicts = [$this->project . '/patches/old.patch' => false, $new => false];

        $result = $this->run_(['drupal/token' => ['Issue #3123456' => 'patches/old.patch']]);

        self::assertCount(1, $result->finding['conflicts']);
        self::assertFileDoesNotExist($new);
    }

    public function testAnUncheckableMergeRequestLeavesThePackageUnpinnedAndNoDownloadBehind(): void
    {
        $this->operations = [self::upgrade('drupal/token', '1.14.0', '1.15.0')];
        $this->issue = new Issue('3123456', 'Fix', '8', 'https://www.drupal.org/node/3123456', 'token');
        $this->drupalCode = $this->drupalCode(fixed: false);
        $this->drupalCode->method('issueForkMergeRequests')->willReturn([new ForkMergeRequest('abc123', self::MERGE_REQUEST)]);
        $new = $this->project . '/patches/token/3123456-abc123-fix.diff';
        $this->verdicts = [$this->project . '/patches/old.patch' => false, $new => new \RuntimeException('down')];

        $result = $this->run_(['drupal/token' => ['Issue #3123456' => 'patches/old.patch']]);

        self::assertSame(['removed' => [], 'updated' => [], 'conflicts' => []], $result->finding);
        self::assertFileDoesNotExist($new);
    }

    public function testAPatchWithoutAMergeRequestToReplaceItIsLeftAsItIs(): void
    {
        $this->operations = [self::upgrade('drupal/token', '1.14.0', '1.15.0')];
        $this->issue = new Issue('3123456', 'Fix', '8', 'https://www.drupal.org/node/3123456', 'token');
        $this->drupalCode = $this->drupalCode(fixed: false);
        $this->drupalCode->method('issueForkMergeRequests')->willReturn([]);
        $this->verdicts = [$this->project . '/patches/old.patch' => false];

        $result = $this->run_(['drupal/token' => ['Issue #3123456' => 'patches/old.patch']]);

        self::assertSame(['removed' => [], 'updated' => [], 'conflicts' => []], $result->finding);
    }

    public function testAFailedForkLookupIsLoggedAndChangesNothing(): void
    {
        $this->operations = [self::upgrade('drupal/token', '1.14.0', '1.15.0')];
        $this->issue = new Issue('3123456', 'Fix', '8', 'https://www.drupal.org/node/3123456', 'token');
        $this->drupalCode = $this->drupalCode(fixed: false);
        $this->drupalCode->method('issueForkMergeRequests')->willThrowException(new \RuntimeException('no fork'));
        $this->verdicts = [$this->project . '/patches/old.patch' => false];

        $result = $this->run_(['drupal/token' => ['Issue #3123456' => 'patches/old.patch']]);

        self::assertSame(['removed' => [], 'updated' => [], 'conflicts' => []], $result->finding);
        self::assertContains(['level' => 'error', 'message' => 'failed to find the issue fork\'s merge requests', 'context' => ['error' => 'no fork']], $this->log->entries());
    }

    public function testTheReportIsTheChangesOrNothing(): void
    {
        $changes = ['removed' => [], 'updated' => [], 'conflicts' => [['package' => 'a/b', 'fixed_version' => '1', 'patch_path' => '', 'patch_description' => 'd', 'new_version' => '2']]];

        self::assertSame($changes, ComposerPatches::report(Records::findings([Records::record('pre-composer-update', $changes)])));
        self::assertNull(ComposerPatches::report(Records::findings([Records::record('pre-composer-update', ['removed' => [], 'updated' => [], 'conflicts' => []])])));
        self::assertNull(ComposerPatches::report(Records::findings()));
    }

    public function testAPatchIsRemoteOnlyOverHttp(): void
    {
        self::assertTrue(ComposerPatches::isRemote('https://example.com/a.patch'));
        self::assertTrue(ComposerPatches::isRemote('HTTP://example.com/a.patch'));
        self::assertFalse(ComposerPatches::isRemote('/patches/a.patch'));
        self::assertFalse(ComposerPatches::isRemote('patches/a.patch'));
        self::assertFalse(ComposerPatches::isRemote('file:///patches/a.patch'));
    }

    public function testAnIssueTitleBecomesAFileNameWithoutAPathSeparator(): void
    {
        self::assertSame('fix_the_..thing', ComposerPatches::fileNameOf('Fix the ../thing'));
        self::assertSame('a-b_c.d', ComposerPatches::fileNameOf('A-B C.d?/'));
    }

    /** @var list<string> */
    private array $downloaded = [];
    private bool $downloadFails = false;

    /**
     * @param array<string, array<string, string>>|object|null $patches
     */
    private function run_(mixed $patches, bool $issueFails = false): HookResult
    {
        return $this->addon($patches === null ? null : json_decode((string) json_encode($patches), false), null, $issueFails)
            ->handle(Event::preComposerUpdate($this->project, new Plan()));
    }

    private function addon(mixed $patches, ?Resolver $resolver = null, bool $issueFails = false): ComposerPatches
    {
        if ($resolver === null) {
            $resolver = $this->createMock(Resolver::class);
            $resolver->method('resolve')->willReturnCallback(function (array $only, array $keep, bool $minimal, bool $write): array {
                self::assertFalse($write, 'composer_patches only dry-runs the update');
                $this->planResolved = new Plan($only, $keep, $minimal);
                return $this->operations;
            });
        }
        $checker = $this->createMock(PatchChecker::class);
        $checker->method('check')->willReturnCallback(function (string $name, string $version, string $reference, array $patches): array {
            $this->checked[] = [$name, $version, $reference, $patches];
            $key = end($patches) . (count($patches) > 1 ? '|together' : '');
            $verdict = $this->verdicts[$key] ?? true;
            if ($verdict instanceof \Exception) {
                throw $verdict;
            }
            return $verdict ? ['outcome' => 'applies'] : ['outcome' => 'rejected', 'patch' => end($patches)];
        });
        $writer = $this->createMock(PatchWriter::class);
        $writer->method('write')->willReturnCallback(function (\stdClass $patches): void {
            $this->written = $patches;
        });
        $downloader = $this->createMock(PatchDownloader::class);
        $downloader->method('download')->willReturnCallback(function (string $url, string $file): void {
            if ($this->downloadFails) {
                throw new \RuntimeException('failed to download ' . $url);
            }
            $this->downloaded[] = $url;
            $this->writeFiles(dirname($file), [basename($file) => 'diff']);
        });
        $drupalOrg = $this->createMock(DrupalOrg::class);
        $drupalOrg->method('issue')->willReturnCallback(fn (string $id): Issue => $issueFails || $this->issue === null
            ? throw new \RuntimeException('failed to fetch issue ' . $id)
            : $this->issue);

        $project = new Project(packages: $this->installed, patches: $patches, dependencyPatches: $this->dependencyPatches);
        return new ComposerPatches($this->log, static fn (): Project => $project, $resolver, $checker, $writer, $downloader, $drupalOrg, $this->drupalCode);
    }

    private function drupalCode(bool $fixed): DrupalCode&\PHPUnit\Framework\MockObject\MockObject
    {
        $drupalCode = $this->createMock(DrupalCode::class);
        $drupalCode->method('isFixedIn')->willReturn($fixed);
        return $drupalCode;
    }

    private function writtenOrProposed(HookResult $result): \stdClass
    {
        return $this->written ?? throw new \LogicException('nothing was written: ' . json_encode($result->finding));
    }

    /**
     * @return array{action: string, package: string, from: string, to: string}
     */
    private static function upgrade(string $package, string $from, string $to): array
    {
        return ['action' => 'Upgrade', 'package' => $package, 'from' => $from, 'to' => $to];
    }

    /**
     * @return list<array{message: string, paths: list<string>}>
     */
    private static function commits(HookResult $result): array
    {
        return array_map(static fn (Commit $commit): array => $commit->toArray(), $result->commits);
    }
}
