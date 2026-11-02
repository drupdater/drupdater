<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests;

use Drupdater\Agent\Failure;

trait Fails
{
    /**
     * @return Failure what $run threw, for a test to assert more on
     */
    private static function assertFails(string $kind, string $reason, \Closure $run): Failure
    {
        try {
            $run();
        }
        catch (Failure $failure) {
            self::assertSame([$kind, $reason], [$failure->kind, $failure->reason]);
            return $failure;
        }
        self::fail('no failure');
    }
}
