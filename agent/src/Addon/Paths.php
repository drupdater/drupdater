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
        $roots = [rtrim($dir, '/')];
        $resolved = realpath($dir);
        if ($resolved !== false) {
            $roots[] = rtrim($resolved, '/');
        }
        foreach ($roots as $root) {
            if ($path === $root) {
                return '.';
            }
            if (str_starts_with($path, $root . '/')) {
                return rtrim(substr($path, strlen($root) + 1), '/');
            }
        }
        return $path;
    }
}
