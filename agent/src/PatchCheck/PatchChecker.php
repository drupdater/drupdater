<?php

declare(strict_types=1);

namespace Drupdater\Agent\PatchCheck;

use Composer\Util\Filesystem;

/**
 * Fetches a package at a version into a scratch directory and applies patches to it in order.
 */
final class PatchChecker
{
    /**
     * @param array<mixed> $patchLevels the project's extra.patchLevel: one level per package
     */
    public function __construct(
        private readonly PackageFetcher $fetcher,
        private readonly PatchFiles $patchFiles,
        private readonly PatchApplier $applier,
        private readonly array $patchLevels = [],
    ) {
    }

    /**
     * @param list<string> $patches
     * @return array{outcome: 'applies'}|array{outcome: 'rejected', patch: string}
     *
     * @throws \Drupdater\Agent\Failure when the package or a patch cannot be obtained
     */
    public function check(string $name, string $version, string $reference, array $patches): array
    {
        $level = $this->patchLevels[$name] ?? null;
        $levels = is_string($level) && $level !== '' ? [$level] : PatchApplier::DEFAULT_LEVELS;

        $filesystem = new Filesystem();
        $scratch = sys_get_temp_dir() . '/drupdater-patch-check-' . bin2hex(random_bytes(8));
        try {
            $packageDir = $scratch . '/package';
            $this->fetcher->fetch($name, $version, $reference, $packageDir);
            foreach ($patches as $patch) {
                $file = $this->patchFiles->local($patch, $scratch . '/patches');
                if (!$this->applier->apply($packageDir, $file, $levels)) {
                    return ['outcome' => 'rejected', 'patch' => $patch];
                }
            }
            return ['outcome' => 'applies'];
        }
        finally {
            $filesystem->removeDirectory($scratch);
        }
    }
}
