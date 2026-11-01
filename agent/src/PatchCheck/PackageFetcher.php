<?php

declare(strict_types=1);

namespace Drupdater\Agent\PatchCheck;

interface PackageFetcher
{
    /**
     * Installs the package's files into $targetDir, which does not exist yet.
     *
     * @throws \Drupdater\Agent\Failure when the package cannot be obtained
     */
    public function fetch(string $name, string $version, string $reference, string $targetDir): void;
}
