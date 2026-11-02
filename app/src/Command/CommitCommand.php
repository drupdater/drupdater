<?php

declare(strict_types=1);

namespace Drupdater\App\Command;

use Drupdater\App\Failure;
use Drupdater\App\Git\CommitFailed;
use Drupdater\App\Git\Committer;
use Drupdater\App\Git\Identity;
use Drupdater\App\Json;
use Drupdater\App\Log;
use Drupdater\App\Pipeline\Commit;

/**
 * Makes the run's own commits of exactly the paths named, as the pipeline makes an addon's.
 *
 * Request: {author{name, email}, commits[{message, paths[]}]}. Result: {committed[]}, the
 * messages of the commits that were made: one whose paths hold no change is not.
 */
final class CommitCommand extends EnvelopeCommand
{
    /**
     * @param \Closure(string, Identity): Committer $committer the committer into dir's index
     */
    public function __construct(Log $log, private readonly \Closure $committer)
    {
        parent::__construct($log);
    }

    protected function configure(): void
    {
        $this->setName('internal:commit')
            ->setDescription('Commits exactly the paths named.')
            ->setHidden(true);
    }

    protected function handle(string $dir, \stdClass $request): array
    {
        $committer = ($this->committer)($dir, Author::fromJson(Json::at($request, 'author')));
        $committed = [];
        foreach (Json::list(Json::at($request, 'commits')) as $entry) {
            $commit = Commit::fromJson($entry);
            // The committer reads an empty path as the whole tree, which would sweep in unrelated changes.
            if ($commit->message === '' || $commit->paths === [] || in_array('', $commit->paths, true)) {
                throw new Failure(Failure::INVALID_REQUEST, 'a commit needs a message and paths, none of them empty');
            }
            try {
                if ($committer->commit($commit->message, $commit->paths)) {
                    $committed[] = $commit->message;
                }
            }
            catch (CommitFailed $e) {
                throw new Failure(Failure::FAILED, 'the commit failed', $e->getMessage(), $e);
            }
        }
        return ['committed' => $committed];
    }
}
