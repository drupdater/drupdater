<?php

declare(strict_types=1);

namespace Drupdater\Agent\Addon;

/**
 * When in an event a hook runs: every prepare hook before any change hook, and so on. Within a
 * stage, hooks run in catalogue order.
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
}
