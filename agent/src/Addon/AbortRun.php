<?php

declare(strict_types=1);

namespace Drupdater\Agent\Addon;

/**
 * Thrown by a hook when the run has nothing to do. Not a failure: drupdater ends the run and
 * reports no changes.
 */
final class AbortRun extends \RuntimeException
{
}
