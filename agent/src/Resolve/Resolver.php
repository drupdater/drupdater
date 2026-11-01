<?php

declare(strict_types=1);

namespace Drupdater\Agent\Resolve;

use Composer\Factory;
use Composer\Json\JsonFile;
use Drupdater\Agent\Addon\Pin;
use Drupdater\Agent\Failure;
use Symfony\Component\Console\Command\Command;
use Symfony\Component\Console\Input\ArrayInput;
use Symfony\Component\Console\Output\OutputInterface;

/**
 * Runs Composer's own `update --no-install` and reports what it changed in the lock: written for
 * the run's update, or put back for a hook that only needs to know what the update would do.
 */
class Resolver
{
    public function __construct(private readonly Command $update, private readonly OutputInterface $output)
    {
    }

    /**
     * @param list<string> $packages restricts the update; empty updates every package
     * @param list<Pin> $keep
     * @param bool $write false puts composer.json and composer.lock back as they were
     * @return list<array{action: string, package: string, from: string, to: string}>
     *
     * @throws Failure when there is no lock, or the update cannot be resolved
     */
    public function resolve(array $packages, array $keep, bool $minimalChanges, bool $write): array
    {
        $composerFile = Factory::getComposerFile();
        $lockFile = Factory::getLockFile($composerFile);
        $lock = new JsonFile($lockFile);
        if (!$lock->exists()) {
            throw new Failure(Failure::FAILED, sprintf('the project has no %s', basename($lockFile)));
        }
        $before = $lock->read();
        $restore = $write ? null : self::snapshot([$composerFile, $lockFile]);

        try {
            $status = $this->update($packages, array_map(self::constraint(...), $keep), $minimalChanges, $write);
            if ($status !== 0) {
                throw new Failure(Failure::FAILED, 'composer update failed', sprintf('exit code %d', $status));
            }
            return LockDiff::between($before, $lock->read());
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
        return $this->update->run($input, $this->output);
    }

    /**
     * @param list<string> $files
     * @return \Closure(): void
     */
    private static function snapshot(array $files): \Closure
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
    private static function constraint(Pin $pin): string
    {
        return $pin->package . ':' . str_replace(' ', '#', $pin->version);
    }
}
