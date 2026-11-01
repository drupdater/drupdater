<?php

declare(strict_types=1);

namespace Drupdater\Agent\PatchCheck;

use Composer\Util\Filesystem;
use Composer\Util\HttpDownloader;
use Drupdater\Agent\Failure;

/**
 * Turns a patch reference -- an absolute path or an http(s) URL -- into a local file.
 */
final class PatchFiles
{
    public const NOT_DOWNLOADABLE = 'a patch could not be downloaded';
    public const NOT_FOUND = 'a patch file does not exist';

    public function __construct(private readonly HttpDownloader $downloader)
    {
    }

    /**
     * @throws Failure when the patch cannot be read
     */
    public function local(string $patch, string $scratchDir): string
    {
        if (!preg_match('{^https?://}i', $patch)) {
            if (!is_file($patch)) {
                throw new Failure(Failure::UNOBTAINABLE, self::NOT_FOUND, $patch);
            }
            return $patch;
        }

        (new Filesystem())->ensureDirectoryExists($scratchDir);
        $file = $scratchDir . '/' . bin2hex(random_bytes(8)) . '.patch';
        try {
            $this->copy($patch, $file);
        }
        catch (\Exception $e) {
            throw new Failure(Failure::UNOBTAINABLE, self::NOT_DOWNLOADABLE, $e->getMessage(), $e);
        }
        return $file;
    }

    // Retried once, as composer-patches 1.x does: a patch host's transient failure is common.
    private function copy(string $url, string $file): void
    {
        try {
            $this->downloader->copy($url, $file);
        }
        catch (\Exception) {
            $this->downloader->copy($url, $file);
        }
    }
}
