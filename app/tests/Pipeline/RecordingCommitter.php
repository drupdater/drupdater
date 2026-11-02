<?php

declare(strict_types=1);

namespace Drupdater\App\Tests\Pipeline;

use Drupdater\App\Git\CommitFailed;
use Drupdater\App\Git\Committer;

/**
 * Makes only the commits whose message is in made, fails on fail, and records every commit asked for.
 */
final class RecordingCommitter implements Committer
{
    /** @var list<array{message: string, paths: list<string>}> */
    public array $commits = [];

    /**
     * @param list<string> $made
     */
    public function __construct(private readonly array $made = [], private readonly ?string $fail = null)
    {
    }

    public function commit(string $message, array $paths): bool
    {
        $this->commits[] = ['message' => $message, 'paths' => $paths];
        if ($message === $this->fail) {
            throw new CommitFailed("failed to commit \"$message\": boom");
        }
        return in_array($message, $this->made, true);
    }

    /**
     * @return list<string>
     */
    public function messages(): array
    {
        return array_column($this->commits, 'message');
    }
}
