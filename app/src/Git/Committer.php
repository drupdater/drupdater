<?php

declare(strict_types=1);

namespace Drupdater\App\Git;

/**
 * How an addon gets its changes onto the update branch: it names the paths it changed, and the
 * committer does the git.
 */
interface Committer
{
    /**
     * Stages paths, files or directories, deletions included, and commits them alone. Returns
     * false without committing when they hold no change.
     *
     * @param list<string> $paths
     * @throws CommitFailed
     */
    public function commit(string $message, array $paths): bool;
}
