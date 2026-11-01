<?php

declare(strict_types=1);

namespace Drupdater\Agent\Command;

use Composer\Factory;
use Composer\Json\JsonFile;
use Drupdater\Agent\Failure;
use Drupdater\Agent\Resolve\LockDiff;
use Symfony\Component\Console\Input\ArrayInput;

/**
 * Runs Composer's own `update --no-install` and reports what it changed in the lock.
 *
 * Request: {packages[], keep[{package, version}], minimal_changes, write}. Result: {changes[]}.
 * Without write, composer.json and composer.lock are put back as they were.
 */
final class ResolveCommand extends AgentCommand
{
    protected function configure(): void
    {
        $this->setName('drupdater:resolve')
            ->setDescription('Resolves an update into composer.lock and reports what changed.')
            ->setHidden(true);
    }

    protected function handle(array $request): array
    {
        $packages = self::strings($request, 'packages');
        $keep = $request['keep'] ?? [];
        if (!is_array($keep) || !array_is_list($keep)) {
            throw new Failure(Failure::INVALID_REQUEST, 'keep must be a list');
        }
        $with = array_map(self::constraint(...), $keep);
        $write = ($request['write'] ?? false) === true;

        $composerFile = Factory::getComposerFile();
        $lockFile = Factory::getLockFile($composerFile);
        $lock = new JsonFile($lockFile);
        if (!$lock->exists()) {
            throw new Failure(Failure::FAILED, sprintf('the project has no %s', basename($lockFile)));
        }
        $before = $lock->read();
        $restore = $write ? null : $this->snapshot([$composerFile, $lockFile]);

        try {
            $status = $this->update($packages, $with, ($request['minimal_changes'] ?? false) === true, $write);
            if ($status !== 0) {
                throw new Failure(Failure::FAILED, 'composer update failed', sprintf('exit code %d', $status));
            }
            return ['changes' => LockDiff::between($before, $lock->read())];
        }
        finally {
            if ($restore !== null) {
                $restore();
            }
        }
    }

    /**
     * The flags drupdater has always run `composer update` with; Composer's own command does the
     * rest, plugins and scripts included.
     *
     * @param list<string> $packages
     * @param list<string> $with
     */
    private function update(array $packages, array $with, bool $minimalChanges, bool $write): int
    {
        $arguments = [
            'packages' => $packages,
            '--with' => $with,
            '--no-install' => true,
            '--no-progress' => true,
            '--with-all-dependencies' => true,
            '--ignore-platform-reqs' => true,
            '--minimal-changes' => $minimalChanges,
        ];
        // A result that is put back runs no scripts: the project never asked for them on it.
        $arguments += $write ? ['--bump-after-update' => 'all'] : ['--no-scripts' => true];

        $input = new ArrayInput($arguments);
        $input->setInteractive(false);
        $update = $this->getApplication()?->find('update');
        if ($update === null) {
            throw new Failure(Failure::INTERNAL, 'Composer has no update command');
        }
        return $update->run($input, $this->output ?? throw new \LogicException('no output'));
    }

    /**
     * @param list<string> $files
     * @return \Closure(): void
     */
    private function snapshot(array $files): \Closure
    {
        $contents = [];
        foreach ($files as $file) {
            $content = @file_get_contents($file);
            if ($content === false) {
                throw new Failure(Failure::FAILED, sprintf('%s could not be read', $file));
            }
            $contents[$file] = $content;
        }
        return static function () use ($contents): void {
            foreach ($contents as $file => $content) {
                file_put_contents($file, $content);
            }
        };
    }

    /**
     * A version as the lock prints it, "1.x-dev 0424dff" for a dev one, as the --with constraint
     * Composer takes, which names a commit as "1.x-dev#0424dff".
     */
    private static function constraint(mixed $pin): string
    {
        if (!is_array($pin) || !is_string($pin['package'] ?? null) || !is_string($pin['version'] ?? null)
            || $pin['package'] === '' || $pin['version'] === '') {
            throw new Failure(Failure::INVALID_REQUEST, 'each keep must have a package and a version');
        }
        return $pin['package'] . ':' . str_replace(' ', '#', $pin['version']);
    }

    /**
     * @param array<string, mixed> $request
     * @return list<string>
     */
    private static function strings(array $request, string $key): array
    {
        $value = $request[$key] ?? [];
        if (!is_array($value) || !array_is_list($value) || array_filter($value, 'is_string') !== $value) {
            throw new Failure(Failure::INVALID_REQUEST, sprintf('%s must be a list of strings', $key));
        }
        return $value;
    }
}
