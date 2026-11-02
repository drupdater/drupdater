<?php

declare(strict_types=1);

namespace Drupdater\App\Tests\Command;

use Drupdater\App\Command\CommitCommand;
use Drupdater\App\Git\Committer;
use Drupdater\App\Git\Identity;
use Drupdater\App\Log;
use Drupdater\App\Tests\Pipeline\RecordingCommitter;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;

final class CommitCommandTest extends TestCase
{
    /** @var list<array{string, Identity}> */
    private array $built = [];

    public function testAnswersTheProtocolExample(): void
    {
        $committer = new RecordingCommitter(['Update configuration default']);

        $tester = Run::command($this->command($committer), Run::fixture('commit/request.json'));

        self::assertSame(Run::fixture('commit/response.json') . "\n", $tester->getDisplay());
        self::assertSame([['message' => 'Update configuration default', 'paths' => ['config/sync']], ['message' => 'Update configuration second', 'paths' => ['config/second']]], $committer->commits);
        self::assertEquals([[(string) getcwd(), new Identity('drupdater', 'drupdater@localhost')]], $this->built);
    }

    public function testAFailedCommitFailsTheCommandAfterTheCommitsBeforeIt(): void
    {
        $committer = new RecordingCommitter(fail: 'Update configuration second');

        $tester = Run::command($this->command($committer), Run::fixture('commit/request.json'));

        self::assertSame(
            '{"protocol":6,"command":"internal:commit","ok":false,"error":{"kind":"failed","reason":"the commit failed","message":"failed to commit \"Update configuration second\": boom"}}' . "\n",
            $tester->getDisplay(),
        );
        self::assertSame(1, $tester->getStatusCode());
        self::assertCount(2, $committer->commits);
    }

    /**
     * @return iterable<string, array{array<string, mixed>}>
     */
    public static function invalid(): iterable
    {
        yield 'no message' => [['paths' => ['a']]];
        yield 'no paths' => [['message' => 'm', 'paths' => []]];
        yield 'an empty path, which would read as the whole tree' => [['message' => 'm', 'paths' => ['a', '']]];
    }

    /**
     * @param array<string, mixed> $commit
     */
    #[DataProvider('invalid')]
    public function testRefusesACommitThatNamesNothingPrecisely(array $commit): void
    {
        $committer = new RecordingCommitter();

        $tester = Run::command($this->command($committer), Run::request(['author' => ['name' => 'n', 'email' => 'e'], 'commits' => [$commit]]));

        self::assertStringContainsString('"reason":"a commit needs a message and paths, none of them empty"', $tester->getDisplay());
        self::assertSame([], $committer->commits);
    }

    public function testRefusesARequestWithoutAnAuthor(): void
    {
        $tester = Run::command($this->command(new RecordingCommitter()), Run::request(['commits' => []]));

        self::assertStringContainsString('"kind":"invalid_request","reason":"author needs a name and an email"', $tester->getDisplay());
    }

    public function testIsHidden(): void
    {
        self::assertTrue($this->command(new RecordingCommitter())->isHidden());
    }

    private function command(Committer $committer): CommitCommand
    {
        return new CommitCommand(new Log(), function (string $dir, Identity $author) use ($committer): Committer {
            $this->built[] = [$dir, $author];
            return $committer;
        });
    }
}
