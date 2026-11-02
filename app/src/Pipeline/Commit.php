<?php

declare(strict_types=1);

namespace Drupdater\App\Pipeline;

use Drupdater\App\Json;

/**
 * One commit a hook asks for, of exactly these paths.
 */
final class Commit
{
    /**
     * @param list<string> $paths
     */
    public function __construct(public readonly string $message, public readonly array $paths)
    {
    }

    public static function fromJson(mixed $commit): self
    {
        return new self(Json::string(Json::at($commit, 'message')), Json::strings(Json::at($commit, 'paths')));
    }
}
