<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\DrupalOrg;

use Drupdater\Agent\DrupalOrg\DrupalCode;
use Drupdater\Agent\DrupalOrg\DrupalOrg;
use Drupdater\Agent\DrupalOrg\ForkMergeRequest;
use Drupdater\Agent\DrupalOrg\Issue;
use Drupdater\Agent\DrupalOrg\IssueNumber;
use Drupdater\Agent\Patches\PatchDownloader;
use Drupdater\Agent\Tests\FakeHttp;
use Drupdater\Agent\Tests\Scratch;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;

/**
 * drupal.org's node API and git.drupalcode.org's GitLab API, as composer_patches asks them.
 */
final class DrupalOrgTest extends TestCase
{
    use Scratch;

    private const NODE = 'https://www.drupal.org/api-d7/node/3123456.json';
    private const API = 'https://git.drupalcode.org/api/v4';

    public function testReadsAnIssue(): void
    {
        $http = new FakeHttp([self::NODE => json_encode([
            'nid' => '3123456', 'title' => 'Token fix', 'field_issue_status' => '7', 'url' => 'https://www.drupal.org/node/3123456',
            'field_project' => ['machine_name' => 'token', 'id' => '1'],
        ])]);

        $issue = (new DrupalOrg($http))->issue('3123456');

        self::assertEquals(new Issue('3123456', 'Token fix', '7', 'https://www.drupal.org/node/3123456', 'token'), $issue);
        self::assertTrue($issue->fixed());
    }

    public function testAnIssueWithNumbersForStringsStillReads(): void
    {
        $http = new FakeHttp([self::NODE => '{"nid":3123456,"title":"T","field_issue_status":1,"url":"u","field_project":{"machine_name":"token"}}']);

        $issue = (new DrupalOrg($http))->issue('3123456');

        self::assertSame(['3123456', '1'], [$issue->id, $issue->status]);
        self::assertFalse($issue->fixed());
    }

    /**
     * @return iterable<string, array{string|int, string}>
     */
    public static function unreadableIssues(): iterable
    {
        yield 'an unknown node' => [404, 'failed to fetch issue 3123456: '];
        yield 'an HTML page' => ['<html>', 'failed to decode issue 3123456: '];
        yield 'no object' => ['"x"', 'failed to decode issue 3123456: not an object'];
    }

    #[DataProvider('unreadableIssues')]
    public function testAnIssueThatCannotBeReadIsAnError(string|int $answer, string $message): void
    {
        $this->expectException(\RuntimeException::class);
        $this->expectExceptionMessage($message);
        (new DrupalOrg(new FakeHttp([self::NODE => $answer])))->issue('3123456');
    }

    /**
     * @return iterable<string, array{string, ?string}>
     */
    public static function texts(): iterable
    {
        yield 'a description' => ['This is a test with issue number #123456', '123456'];
        yield 'none' => ['No issue number here', null];
        yield 'the first of two' => ['Multiple issues #111111 and #222222', '111111'];
        yield 'a dated file name' => ['https://www.drupal.org/files/issues/2022-10-04/password_policy_field_last_password_reset_unknown_error_2771129-134.patch', '2771129'];
        yield 'too short to be an issue' => ['Fix for 12345 and comment 134', null];
    }

    #[DataProvider('texts')]
    public function testFindsTheIssueNumberInAText(string $text, ?string $number): void
    {
        self::assertSame($number, IssueNumber::find($text));
    }

    public function testAFixIsACommitOnTheRefNamingTheIssue(): void
    {
        $search = self::API . '/projects/project%2Fdrupal/-/search?scope=commits&search=123456&ref=11.1.0';
        $http = new FakeHttp([$search => '[{"id":"5678"}]']);

        self::assertTrue((new DrupalCode($http, 'secret'))->isFixedIn('drupal', '123456', '11.1.0'));
        self::assertSame(['PRIVATE-TOKEN: secret'], $http->requests[0]['options']['http']['header']);
        self::assertFalse((new DrupalCode(new FakeHttp([$search => '[]']), 'secret'))->isFixedIn('drupal', '123456', '11.1.0'));
    }

    public function testAFailedSearchIsAnErrorNotAMissingFix(): void
    {
        $this->expectExceptionMessage('failed to search drupal for issue 123456');
        (new DrupalCode(new FakeHttp(), 'secret'))->isFixedIn('drupal', '123456', '11.1.0');
    }

    public function testListsTheMergeRequestsFromTheIssueForkOnly(): void
    {
        $http = new FakeHttp([
            self::API . '/projects/issue%2Fdrupal-123456' => '{"id":5678}',
            self::API . '/projects/project%2Fdrupal/merge_requests?source_project_id=5678' => json_encode([
                ['sha' => '111111', 'web_url' => 'https://git.drupalcode.org/project/drupal/-/merge_requests/2'],
                ['sha' => '222222', 'web_url' => 'https://git.drupalcode.org/project/drupal/-/merge_requests/1'],
            ]),
        ]);

        self::assertEquals([
            new ForkMergeRequest('111111', 'https://git.drupalcode.org/project/drupal/-/merge_requests/2'),
            new ForkMergeRequest('222222', 'https://git.drupalcode.org/project/drupal/-/merge_requests/1'),
        ], (new DrupalCode($http, 'secret'))->issueForkMergeRequests('drupal', '123456'));
    }

    public function testAnIssueWithoutAForkIsAnErrorAndAsksNoFurther(): void
    {
        $http = new FakeHttp();
        try {
            (new DrupalCode($http, 'secret'))->issueForkMergeRequests('drupal', '123456');
            self::fail('a missing fork was taken for one without merge requests');
        }
        catch (\RuntimeException $e) {
            self::assertStringStartsWith('failed to get the fork of issue 123456', $e->getMessage());
        }
        self::assertCount(1, $http->requests);
    }

    public function testAFailedMergeRequestQueryIsAnError(): void
    {
        $this->expectExceptionMessage('failed to list the merge requests from the fork of issue 123456');
        (new DrupalCode(new FakeHttp([self::API . '/projects/issue%2Fdrupal-123456' => '{"id":5678}']), 'secret'))->issueForkMergeRequests('drupal', '123456');
    }

    public function testAProjectPathIsEscapedAsGitLabsOwnClientEscapesIt(): void
    {
        $http = new FakeHttp([self::API . '/projects/project%2Fa%2Eb/-/search?scope=commits&search=1&ref=r' => '[]']);

        self::assertFalse((new DrupalCode($http, 'secret'))->isFixedIn('a.b', '1', 'r'));
    }

    public function testDownloadsAPatchCreatingItsDirectory(): void
    {
        $project = $this->scratchDir();

        (new PatchDownloader(new FakeHttp(['https://example.com/a.diff' => 'diff'])))->download('https://example.com/a.diff', $project . '/patches/token/a.diff');

        self::assertSame('diff', file_get_contents($project . '/patches/token/a.diff'));
    }

    public function testAFailedDownloadLeavesNothingBehind(): void
    {
        $project = $this->scratchDir();
        try {
            (new PatchDownloader(new FakeHttp(['https://example.com/a.diff' => 404])))->download('https://example.com/a.diff', $project . '/patches/a.diff');
            self::fail('a 404 was downloaded');
        }
        catch (\RuntimeException $e) {
            self::assertStringStartsWith('failed to download https://example.com/a.diff', $e->getMessage());
        }
        self::assertFileDoesNotExist($project . '/patches/a.diff');
    }
}
