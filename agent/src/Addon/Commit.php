<?php

declare(strict_types=1);

namespace Drupdater\Agent\Addon;

/**
 * One commit a hook asks for. drupdater makes it, of exactly these paths, before the next hook
 * runs; paths that hold no change make no commit.
 */
final class Commit
{
    /**
     * @param list<string> $paths relative to the project, files or directories, deletions included
     */
    public function __construct(public readonly string $message, public readonly array $paths)
    {
    }

    /**
     * @return array{message: string, paths: list<string>}
     */
    public function toArray(): array
    {
        return ['message' => $this->message, 'paths' => $this->paths];
    }
}
