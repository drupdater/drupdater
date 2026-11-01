<?php

declare(strict_types=1);

namespace Drupdater\Agent\Patches;

use Composer\Util\Filesystem;
use Composer\Util\HttpDownloader;

/**
 * Downloads a patch into the project, through the project's own HTTP settings.
 */
class PatchDownloader
{
    public function __construct(private readonly HttpDownloader $http)
    {
    }

    /**
     * @throws \RuntimeException when the download fails; nothing is left behind then
     */
    public function download(string $url, string $file): void
    {
        (new Filesystem())->ensureDirectoryExists(dirname($file));
        try {
            $this->http->copy($url, $file);
        }
        catch (\Exception $e) {
            @unlink($file);
            throw new \RuntimeException(sprintf('failed to download %s: %s', $url, $e->getMessage()), 0, $e);
        }
    }
}
