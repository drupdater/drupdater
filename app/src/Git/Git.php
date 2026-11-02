<?php

declare(strict_types=1);

namespace Drupdater\App\Git;

use Symfony\Component\Process\Process;

/**
 * The git CLI in one working copy, independent of the git config of whoever owns it: a checkout
 * mounted into the container is owned by another user, which git refuses as "dubious ownership",
 * and the project's hooks and signing setup are for its developers' commits, not drupdater's.
 */
final class Git
{
    private const COMMON = ['-c', 'safe.directory=*', '-c', 'core.hooksPath=/dev/null', '-c', 'commit.gpgSign=false'];

    public function __construct(public readonly string $dir)
    {
    }

    /**
     * Stages paths, files or directories, deletions included. A path that neither exists nor is
     * tracked, or that git ignores, has nothing to stage and is skipped.
     *
     * @param list<string> $paths
     */
    public function stage(array $paths): void
    {
        $stageable = $this->stageable($paths);
        // With no pathspec at all, git add --all would stage the whole tree.
        if ($stageable === []) {
            return;
        }
        $this->run(['add', '--all', '--', ...array_map(static fn (string $path): string => ':(literal)' . $path, $stageable)]);
    }

    /**
     * @return list<string>
     */
    public function staged(): array
    {
        return self::splitZ($this->run(['diff', '--cached', '--name-only', '-z']));
    }

    /**
     * Commits what is staged, by author.
     */
    public function commit(string $message, Identity $author): void
    {
        $this->run(['commit', '--quiet', '--no-verify', '--cleanup=verbatim', '--file', '-'], $message, [
            'GIT_AUTHOR_NAME' => $author->name,
            'GIT_AUTHOR_EMAIL' => $author->email,
            'GIT_COMMITTER_NAME' => $author->name,
            'GIT_COMMITTER_EMAIL' => $author->email,
        ]);
    }

    /**
     * The working copy's own git directory: a linked worktree's, not the main checkout's.
     */
    public function gitDir(): string
    {
        return trim($this->run(['rev-parse', '--absolute-git-dir']));
    }

    /**
     * @param list<string> $paths
     * @return list<string>
     */
    private function stageable(array $paths): array
    {
        $known = array_values(array_filter($paths, fn (string $path): bool => file_exists($this->dir . '/' . $path)
            || is_link($this->dir . '/' . $path)
            || $this->process(['ls-files', '--error-unmatch', '--', ':(literal)' . $path])->run() === 0));
        if ($known === []) {
            return [];
        }
        // Without --no-index a tracked file is never reported, which is what git add does too.
        $check = $this->process(['check-ignore', '--stdin', '-z'], implode("\0", $known) . "\0");
        $check->run();
        if (!in_array($check->getExitCode(), [0, 1], true)) {
            throw new GitError('failed to read which paths git ignores: ' . trim($check->getErrorOutput()));
        }
        $ignored = self::splitZ($check->getOutput());
        return array_values(array_diff($known, $ignored));
    }

    /**
     * @param list<string> $args
     * @param array<string, string> $env
     * @throws GitError carrying git's stderr
     */
    private function run(array $args, ?string $input = null, array $env = []): string
    {
        $process = $this->process($args, $input, $env);
        if ($process->run() !== 0) {
            // Some refusals, such as a commit with nothing staged, are printed on stdout.
            $reason = trim($process->getErrorOutput()) ?: trim($process->getOutput());
            throw new GitError(sprintf('git %s: exit code %s: %s', $args[0], $process->getExitCode(), $reason));
        }
        return $process->getOutput();
    }

    /**
     * @param list<string> $args
     * @param array<string, string> $env
     */
    private function process(array $args, ?string $input = null, array $env = []): Process
    {
        // Unanswered, a credential prompt would hang a CI job until its timeout.
        return new Process(['git', ...self::COMMON, ...$args], $this->dir, ['GIT_TERMINAL_PROMPT' => '0'] + $env, $input, null);
    }

    /**
     * @return list<string>
     */
    private static function splitZ(string $out): array
    {
        return array_values(array_filter(explode("\0", $out), static fn (string $s): bool => $s !== ''));
    }
}
