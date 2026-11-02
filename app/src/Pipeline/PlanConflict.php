<?php

declare(strict_types=1);

namespace Drupdater\App\Pipeline;

/**
 * Two proposals for the update that cannot both hold.
 */
final class PlanConflict extends \RuntimeException
{
}
