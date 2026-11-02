<?php

declare(strict_types=1);

namespace Drupdater\Agent\Command;

use Composer\Factory;
use Drupdater\Agent\Failure;
use Drupdater\Agent\Json;
use Drupdater\Agent\Process\ProcessRunner;
use Drupdater\Agent\Project\ProjectFacts;
use Drupdater\Agent\Site\TestDatabase;
use Drupdater\Agent\Tools\Bin;
use Drupdater\Agent\Tools\Drush;

/**
 * A command about one site of the project: every request names it as `site`, the directory under
 * web/sites/ and the SITE_NAME its settings.php reads.
 */
abstract class SiteCommand extends AgentCommand
{
    /**
     * @param array<string, mixed> $request
     * @return array<string, mixed>
     */
    abstract protected function handleSite(string $site, array $request): array;

    protected function handle(array $request): array
    {
        $site = $request['site'] ?? null;
        if (!is_string($site) || $site === '' || $site === '.' || $site === '..' || str_contains($site, '/')) {
            throw new Failure(Failure::INVALID_REQUEST, 'site must name a directory under sites/');
        }
        return $this->handleSite($site, $request);
    }

    protected function drush(): Drush
    {
        $bin = Json::string($this->requireComposer()->getConfig()->get('bin-dir'));
        return new Drush(new ProcessRunner(), new Bin($bin), $this->projectDir(), Drush::include());
    }

    /**
     * @param array<string, mixed> $request
     */
    protected function testDatabase(array $request): TestDatabase
    {
        $dataDir = $request['data_dir'] ?? null;
        if (!is_string($dataDir) || !str_starts_with($dataDir, '/') || !is_dir($dataDir)) {
            throw new Failure(Failure::INVALID_REQUEST, 'data_dir must be an existing absolute directory');
        }
        $project = (new ProjectFacts($this->requireComposer(), $this->composerFile()))->project();
        return new TestDatabase($this->drush(), $this->log, $this->projectDir(), $project->webRoot, rtrim($dataDir, '/'));
    }

    protected function projectDir(): string
    {
        return dirname($this->composerFile());
    }

    private function composerFile(): string
    {
        $file = Factory::getComposerFile();
        return (string) (realpath($file) ?: $file);
    }
}
