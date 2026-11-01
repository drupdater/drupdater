<?php

declare(strict_types=1);

namespace Drupdater\Agent\PatchCheck;

use Composer\Util\ProcessExecutor;

/**
 * Applies a patch the way cweagans/composer-patches 1.x does, so a verdict here is the verdict
 * the project's own `composer install` would reach.
 */
final class PatchApplier
{
    /** composer-patches 1.x's order: p1 is the likeliest, p4 is for Magento 2 patches. */
    public const DEFAULT_LEVELS = ['-p1', '-p0', '-p2', '-p4'];

    public function __construct(private readonly ProcessExecutor $process)
    {
    }

    /**
     * @param list<string> $levels
     */
    public function apply(string $dir, string $patch, array $levels): bool
    {
        // composer-patches uses git apply only on a source install, then falls back to patch.
        if (is_dir($dir . '/.git') && $this->applyWithGit($dir, $patch, $levels)) {
            return true;
        }
        foreach ($levels as $level) {
            // A dry run first, unlike composer-patches: a failed level must not leave hunks behind
            // for the next level, or for the next patch in a set. --forward refuses a reversed
            // patch instead of prompting on a terminal there is none of.
            $patchArgs = ['patch', $level, '--forward', '--no-backup-if-mismatch', '-d', $dir, '-i', $patch];
            if ($this->run([...$patchArgs, '--dry-run'])) {
                return $this->run($patchArgs);
            }
        }
        return false;
    }

    /**
     * @param list<string> $levels
     */
    private function applyWithGit(string $dir, string $patch, array $levels): bool
    {
        foreach ($levels as $level) {
            if (!$this->run(['git', '-C', $dir, 'apply', '--check', '-v', $level, $patch])) {
                continue;
            }
            // git reports success yet skips the patch in some cases (composer-patches #165).
            if (str_starts_with($this->process->getErrorOutput(), 'Skipped')) {
                continue;
            }
            return $this->run(['git', '-C', $dir, 'apply', $level, $patch]);
        }
        return false;
    }

    /**
     * @param list<string> $command
     */
    private function run(array $command): bool
    {
        $output = '';
        return $this->process->execute($command, $output) === 0;
    }
}
