<?php

declare(strict_types=1);

namespace Drupdater\Agent\Command;

use Composer\Factory;
use Drupdater\Agent\Project\ProjectFacts;

/**
 * Reports what drupdater needs to know about the project. Request: {}. Result: see ProjectFacts.
 */
final class ProjectCommand extends AgentCommand
{
    protected function configure(): void
    {
        $this->setName('drupdater:project')
            ->setDescription('Reports the project\'s layout, plugins, patches and locked packages.')
            ->setHidden(true);
    }

    protected function handle(array $request): array
    {
        $composer = $this->requireComposer();
        return (new ProjectFacts($composer, (string) realpath(Factory::getComposerFile())))->toArray();
    }
}
