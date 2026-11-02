<?php

declare(strict_types=1);

namespace Drupdater\App\Pipeline;

/**
 * A finding one addon's hook recorded.
 */
final class Recorded
{
    public function __construct(public readonly string $addon, public readonly Finding $finding)
    {
    }

    /**
     * @return array{addon: string, finding: array{event: string, site: string, finding: mixed, committed: list<string>}}
     */
    public function toJson(): array
    {
        return ['addon' => $this->addon, 'finding' => $this->finding->toJson()];
    }
}
