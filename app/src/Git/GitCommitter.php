<?php

declare(strict_types=1);

namespace Drupdater\App\Git;

/**
 * Commits into one working copy's index. Sites update concurrently, each hook in a process of its
 * own, so every commit holds an exclusive lock in the working copy's git directory.
 */
final class GitCommitter implements Committer
{
    public function __construct(private readonly Git $git, private readonly Identity $author)
    {
    }

    public function commit(string $message, array $paths): bool
    {
        try {
            return $this->locked(fn (): bool => $this->commitLocked($message, $paths));
        }
        catch (GitError $e) {
            throw new CommitFailed(sprintf('failed to commit "%s": %s', $message, $e->getMessage()), 0, $e);
        }
    }

    /**
     * @param list<string> $paths
     */
    private function commitLocked(string $message, array $paths): bool
    {
        $this->git->stage($paths);
        $staged = $this->git->staged();
        $foreign = array_values(array_filter($staged, static fn (string $file): bool => !Paths::anyContains($paths, $file)));
        // Refused rather than committed, or this commit would carry another change under its message.
        if ($foreign !== []) {
            sort($foreign, SORT_STRING);
            throw new CommitFailed(sprintf('refusing to commit "%s": %s already staged', $message, implode(', ', $foreign)));
        }
        if ($staged === []) {
            return false;
        }
        $this->git->commit($message, $this->author);
        return true;
    }

    /**
     * @param callable(): bool $commit
     */
    private function locked(callable $commit): bool
    {
        $path = $this->git->gitDir() . '/drupdater.lock';
        $lock = @fopen($path, 'c');
        if ($lock === false) {
            throw new GitError(sprintf('failed to open the lock %s', $path));
        }
        try {
            flock($lock, LOCK_EX);
            return $commit();
        }
        finally {
            fclose($lock);
        }
    }
}
