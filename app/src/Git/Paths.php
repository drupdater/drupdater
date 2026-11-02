<?php

declare(strict_types=1);

namespace Drupdater\App\Git;

final class Paths
{
    /**
     * Whether file is dir or lives under it: not a substring test, which would match the sibling
     * "translations-backup/de.po" for dir "translations". An empty dir contains everything.
     */
    public static function contains(string $dir, string $file): bool
    {
        $dir = trim($dir, '/');
        if ($dir === '') {
            return true;
        }
        $file = trim($file, '/');
        return $file === $dir || str_starts_with($file, $dir . '/');
    }

    /**
     * @param list<string> $dirs
     */
    public static function anyContains(array $dirs, string $file): bool
    {
        foreach ($dirs as $dir) {
            if (self::contains($dir, $file)) {
                return true;
            }
        }
        return false;
    }
}
