<?php

declare(strict_types=1);

namespace Drupdater\Agent\Command;

use Drupdater\Agent\Addon\Catalogue;
use Drupdater\Agent\Config\ProjectConfig;
use Drupdater\Agent\Failure;

/**
 * Reads a project's .drupdater.yaml, checks it against the addon catalogue, and answers the
 * configuration a run uses. Needs no project: the file is named by its absolute path.
 *
 * Request: {path}. Result: {found, sites, timeout_ns, run_types{normal, security}}, each run type
 * with the addons it runs, mandatory ones first, and auto_merge. A file that fails a check is
 * failed, its reason saying what to fix.
 */
final class ConfigCommand extends AgentCommand
{
    private readonly Catalogue $catalogue;

    public function __construct(?Catalogue $catalogue = null)
    {
        $this->catalogue = $catalogue ?? Catalogue::builtin();
        parent::__construct();
    }

    protected function configure(): void
    {
        $this->setName('drupdater:config')
            ->setDescription('Reads and checks a project\'s .drupdater.yaml.')
            ->setHidden(true);
    }

    protected function handle(array $request): array
    {
        $path = $request['path'] ?? null;
        if (!is_string($path) || !str_starts_with($path, '/')) {
            throw new Failure(Failure::INVALID_REQUEST, 'path must be an absolute path');
        }
        return ProjectConfig::load($path, $this->catalogue)->toArray();
    }
}
