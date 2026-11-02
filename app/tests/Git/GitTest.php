<?php

declare(strict_types=1);

namespace Drupdater\App\Tests\Git;

use Drupdater\App\Git\Git;
use Drupdater\App\Git\GitError;
use Drupdater\App\Git\Identity;
use PHPUnit\Framework\TestCase;

final class GitTest extends TestCase
{
    public function testStagesADirectoryDeletionsIncluded(): void
    {
        $repo = new Repository(['dir/a' => 'a', 'dir/b' => 'b']);
        $repo->write('dir/a', 'changed');
        $repo->remove('dir/b');
        $repo->write('dir/c', 'new');

        (new Git($repo->dir))->stage(['dir']);

        self::assertSame(['dir/a', 'dir/b', 'dir/c'], $repo->staged());
    }

    public function testStagesADeletedFileNamedOnItsOwn(): void
    {
        $repo = new Repository(['gone.txt' => 'x']);
        $repo->remove('gone.txt');

        (new Git($repo->dir))->stage(['gone.txt']);

        self::assertSame(['gone.txt'], $repo->staged());
    }

    public function testSkipsAPathThatNeitherExistsNorIsTrackedAndAnIgnoredOne(): void
    {
        $repo = new Repository(['.gitignore' => "ignored.txt\n"]);
        $repo->write('ignored.txt', 'x');
        $repo->write('new.txt', 'x');

        (new Git($repo->dir))->stage(['missing.txt', 'ignored.txt', 'new.txt']);

        self::assertSame(['new.txt'], $repo->staged());
    }

    public function testStagesADanglingSymlinkAndATrackedFileWhoseNameLooksLikeAPattern(): void
    {
        $repo = new Repository(['[a].txt' => 'x', 'a.txt' => 'a']);
        $repo->remove('[a].txt');
        symlink('missing-target', $repo->dir . '/link');

        (new Git($repo->dir))->stage(['[a].txt', 'link']);

        self::assertSame(['[a].txt', 'link'], $repo->staged());
    }

    public function testOnlySkippedPathsStageNothingRatherThanEverything(): void
    {
        $repo = new Repository(['a.txt' => 'a', '.gitignore' => "ignored.txt\n"]);
        $repo->write('a.txt', 'changed');
        $repo->write('ignored.txt', 'x');

        (new Git($repo->dir))->stage(['missing.txt', 'ignored.txt']);
        (new Git($repo->dir))->stage([]);

        self::assertSame([], $repo->staged());
    }

    public function testAPathIsLiteralNeverAPattern(): void
    {
        $repo = new Repository(['a.txt' => 'a', '*.txt' => 'star']);
        $repo->write('a.txt', 'changed');
        $repo->write('*.txt', 'changed');

        (new Git($repo->dir))->stage(['*.txt']);

        self::assertSame(['*.txt'], $repo->staged());
    }

    public function testStagedListsWhatIsStagedOnly(): void
    {
        $repo = new Repository(['a.txt' => 'a', 'b.txt' => 'b']);
        $repo->write('a.txt', 'changed');
        $repo->write('b.txt', 'changed');
        $repo->git('add', 'b.txt');

        self::assertSame(['b.txt'], (new Git($repo->dir))->staged());
    }

    public function testCommitIsByTheAuthorWithTheMessageVerbatimIgnoringHooksAndSigning(): void
    {
        $repo = new Repository(['a.txt' => 'a']);
        $repo->git('config', 'core.hooksPath', 'hooks');
        $repo->write('hooks/pre-commit', "#!/bin/sh\nexit 1\n");
        chmod($repo->dir . '/hooks/pre-commit', 0o755);
        $repo->git('config', 'commit.gpgSign', 'true');
        $repo->write('a.txt', 'changed');
        $repo->git('add', 'a.txt');
        $message = "Subject\n\n# not a comment\n";

        (new Git($repo->dir))->commit($message, new Identity('Drupdater Bot', 'bot@example.com'));

        self::assertSame($message, $repo->git('log', '-1', '--format=%B') . "\n");
        self::assertSame('Drupdater Bot <bot@example.com> Drupdater Bot <bot@example.com>', $repo->git('log', '-1', '--format=%an <%ae> %cn <%ce>'));
    }

    public function testACommitWithNothingStagedFailsWithGitsReason(): void
    {
        $repo = new Repository(['a.txt' => 'a']);

        $this->expectException(GitError::class);
        $this->expectExceptionMessageMatches('/^git commit: exit code 1: \S.*nothing to commit.*\S$/s');

        (new Git($repo->dir))->commit('m', new Identity('n', 'e'));
    }

    public function testGitDirIsALinkedWorktreesOwn(): void
    {
        $repo = new Repository();
        $worktree = $repo->dir . '/wt';
        $repo->git('worktree', 'add', '--quiet', '--detach', $worktree, 'HEAD');

        self::assertSame(realpath($repo->dir) . '/.git/worktrees/wt', (new Git($worktree))->gitDir());
    }

    public function testFailsOutsideARepository(): void
    {
        $dir = sys_get_temp_dir() . '/drupdater-app-norepo-' . bin2hex(random_bytes(4));
        mkdir($dir);
        file_put_contents($dir . '/a.txt', 'a');
        try {
            $this->expectException(GitError::class);
            (new Git($dir))->stage(['a.txt']);
        }
        finally {
            unlink($dir . '/a.txt');
            rmdir($dir);
        }
    }
}
