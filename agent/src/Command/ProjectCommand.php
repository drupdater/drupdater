<?php

declare(strict_types=1);

namespace Drupdater\Agent\Command;

use Composer\Factory;
use Drupdater\Agent\Project\ProjectFacts;

/**
 * Reports what drupdater's own phases read about the project: its web root, whose scaffold files
 * an update rewrites, and the lock's content-hash, which names the update branch.
 *
 * Request: {}. Result: {web_root, lock_hash}, each null where the project has none.
 */
final class ProjectCommand extends AgentCommand
{
    protected function configure(): void
    {
        $this->setName('drupdater:project')
            ->setDescription('Reports the project\'s web root and the lock\'s content-hash.')
            ->setHidden(true);
    }

    protected function handle(array $request): array
    {
        $project = (new ProjectFacts($this->requireComposer(), (string) realpath(Factory::getComposerFile())))->project();
        return ['web_root' => $project->webRoot, 'lock_hash' => $project->lockHash];
    }
}
