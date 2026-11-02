<?php

declare(strict_types=1);

namespace Drupdater\App\Pipeline;

/**
 * Orders the hooks of one event: every prepare hook runs before any change hook, and so on.
 * Within a stage, hooks run in addon order.
 */
enum Stage: string
{
    /** Produces what later hooks of the same event read. */
    case Prepare = 'prepare';
    /** Edits the project. */
    case Change = 'change';
    /** Rewrites what change hooks wrote into the project's style. */
    case Format = 'format';
    /** Reads the final state and changes nothing. */
    case Inspect = 'inspect';

    public function order(): int
    {
        return (int) array_search($this, self::cases(), true);
    }
}
