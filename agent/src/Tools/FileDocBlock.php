<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tools;

/**
 * Moves a file's @file docblock back above the use statements Rector imported in front of it. In
 * a file without a namespace Rector puts new imports first, ahead of the docblock, and Drupal's
 * coding standard then has PHPCBF add a second, empty @file docblock.
 */
final class FileDocBlock
{
    public static function first(string $src): string
    {
        $lines = preg_split('/(?<=\n)/', $src) ?: [$src];
        if (trim($lines[0]) !== '<?php') {
            return $src;
        }

        $count = count($lines);
        $i = self::skipBlank($lines, 1);
        $useStart = $i;
        while ($i < $count && self::isUse($lines[$i])) {
            $i++;
        }
        if ($i === $useStart) {
            return $src;
        }
        $useEnd = $i;

        $docStart = self::skipBlank($lines, $i);
        if ($docStart === $count || !str_starts_with(trim($lines[$docStart]), '/**')) {
            return $src;
        }
        $docEnd = $docStart;
        while ($docEnd < $count && !str_ends_with(trim($lines[$docEnd]), '*/')) {
            $docEnd++;
        }
        if ($docEnd === $count) {
            return $src;
        }
        $docEnd++;
        $docBlock = implode('', array_slice($lines, $docStart, $docEnd - $docStart));
        if (!str_contains($docBlock, '@file')) {
            return $src;
        }

        $newline = str_ends_with($lines[0], "\r\n") ? "\r\n" : "\n";
        return $lines[0] . $newline . $docBlock . $newline
            . implode('', array_slice($lines, $useStart, $useEnd - $useStart))
            . implode('', array_slice($lines, $docEnd));
    }

    /**
     * @param list<string> $lines
     */
    private static function skipBlank(array $lines, int $i): int
    {
        while ($i < count($lines) && trim($lines[$i]) === '') {
            $i++;
        }
        return $i;
    }

    private static function isUse(string $line): bool
    {
        $trimmed = trim($line);
        return str_starts_with($trimmed, 'use ') && str_ends_with($trimmed, ';');
    }
}
