<?php

declare(strict_types=1);

namespace Drupdater\App\Tests\Git;

use Drupdater\App\Git\CommitFailed;
use Drupdater\App\Git\Git;
use Drupdater\App\Git\GitCommitter;
use Drupdater\App\Git\Identity;
use PHPUnit\Framework\TestCase;
use Symfony\Component\Process\Process;

final class GitCommitterTest extends TestCase
{
    public function testCommitsExactlyThePathsNamed(): void
    {
        $repo = new Repository([
            'composer.json' => '{}',
            'patches/old.patch' => 'old',
            'translations/de.po' => 'de',
            'translations-backup/x' => 'x',
        ]);
        $repo->write('composer.json', '{"changed":true}');
        $repo->remove('patches/old.patch');
        $repo->write('patches/new.patch', 'new');
        $repo->write('translations/fr.po', 'fr');
        $repo->write('translations-backup/x', 'changed but not named');
        $repo->write('scratch.txt', 'untracked and not named');
        $committer = self::committer($repo);

        $made = $committer->commit('Update patches', ['composer.json', 'composer.lock', 'patches/old.patch', 'patches/new.patch', 'translations']);

        self::assertTrue($made);
        self::assertSame(['Update patches', ['composer.json', 'patches/new.patch', 'patches/old.patch', 'translations/fr.po']], $repo->head(),
            'a deletion, a new file and a directory are committed; an unnamed sibling and a missing path are not');
        self::assertSame('M translations-backup/x', $repo->git('status', '--porcelain', '--untracked-files=no'), 'an unnamed change stays in the worktree');

        $repo->write('translations-backup/x', 'now named');
        self::assertTrue($committer->commit('Back up translations', ['translations-backup']), 'the first commit released the lock');
        self::assertSame(['Back up translations', ['translations-backup/x']], $repo->head());
    }

    public function testPathsWithoutChangesMakeNoCommit(): void
    {
        $repo = new Repository(['composer.json' => '{}']);
        $before = $repo->git('rev-parse', 'HEAD');

        self::assertFalse(self::committer($repo)->commit('Update patches', ['composer.json', 'composer.lock']));
        self::assertSame($before, $repo->git('rev-parse', 'HEAD'), 'no empty commit');
    }

    public function testRefusesToCarryAnotherStagedChange(): void
    {
        $repo = new Repository(['composer.json' => '{}', 'web/a.php' => 'a', 'b.txt' => 'b']);
        $before = $repo->git('rev-parse', 'HEAD');
        $repo->write('composer.json', '{"changed":true}');
        $repo->write('b.txt', 'changed');
        $repo->git('add', 'composer.json', 'b.txt');
        $repo->write('web/a.php', 'fixed');

        try {
            self::committer($repo)->commit('Update coding styles', ['web/a.php']);
            self::fail('committed another staged change');
        }
        catch (CommitFailed $e) {
            self::assertSame('refusing to commit "Update coding styles": b.txt, composer.json already staged', $e->getMessage());
        }
        self::assertSame($before, $repo->git('rev-parse', 'HEAD'));
    }

    public function testAGitFailureIsACommitFailureNamingTheCommit(): void
    {
        $dir = sys_get_temp_dir() . '/drupdater-app-norepo-' . bin2hex(random_bytes(4));
        mkdir($dir);
        try {
            $this->expectException(CommitFailed::class);
            $this->expectExceptionMessageMatches('/^failed to commit "m": git rev-parse: /');
            (new GitCommitter(new Git($dir), new Identity('n', 'e')))->commit('m', ['a']);
        }
        finally {
            rmdir($dir);
        }
    }

    public function testConcurrentCommitsFromSeveralProcessesAllLand(): void
    {
        $repo = new Repository(['README' => 'r']);
        $processes = [];
        foreach (range(1, 8) as $i) {
            $repo->write("site$i/config.yml", "site $i");
            $process = new Process([PHP_BINARY, __DIR__ . '/../fixtures/commit.php', $repo->dir, "Update configuration site$i", "site$i"]);
            $process->start();
            $processes[] = $process;
        }
        foreach ($processes as $process) {
            $process->wait();
            self::assertSame('', $process->getErrorOutput());
            self::assertSame("true\n", $process->getOutput());
        }

        self::assertSame('9', $repo->git('rev-list', '--count', 'HEAD'));
        self::assertSame('', $repo->git('status', '--porcelain'));
    }

    private static function committer(Repository $repo): GitCommitter
    {
        return new GitCommitter(new Git($repo->dir), new Identity('Drupdater Bot', 'bot@example.com'));
    }
}
