<?php

declare(strict_types=1);

namespace Drupdater\Agent\Addon;

/**
 * Relates the absolute paths drush reports to the project, which is tried as given and resolved:
 * drush reports the resolved Drupal root, settings.php whatever it was written as.
 */
final class Paths
{
    public static function relativeTo(string $dir, string $path): string
    {
        if (!str_starts_with($path, '/')) {
            return $path;
        }
        // Git stages "web/../translations" as "translations/…", so the path names what git will.
        $clean = self::clean($path);
        $roots = [self::clean($dir)];
        $resolved = realpath($dir);
        if ($resolved !== false) {
            $roots[] = self::clean($resolved);
        }
        foreach ($roots as $root) {
            if ($clean === $root) {
                return '.';
            }
            $prefix = $root === '/' ? '/' : $root . '/';
            if (str_starts_with($clean, $prefix)) {
                return substr($clean, strlen($prefix));
            }
        }
        return $path;
    }

    private static function clean(string $absolute): string
    {
        $parts = [];
        foreach (explode('/', $absolute) as $part) {
            if ($part === '' || $part === '.') {
                continue;
            }
            if ($part === '..') {
                array_pop($parts);
                continue;
            }
            $parts[] = $part;
        }
        return '/' . implode('/', $parts);
    }
}
