<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tools;

/**
 * A directory of executables and how to run one from it, as `composer exec` does: with that
 * directory first on PATH, so a tool finds its siblings.
 */
final class Bin
{
    public function __construct(public readonly string $dir)
    {
    }

    /**
     * @return list<string>
     */
    public function command(string $tool): array
    {
        return [$this->dir . '/' . $tool];
    }

    /**
     * @return array<string, string>
     */
    public function env(): array
    {
        $path = getenv('PATH');
        return ['PATH' => $this->dir . ($path === false || $path === '' ? '' : PATH_SEPARATOR . $path)];
    }
}
