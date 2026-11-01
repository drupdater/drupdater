<?php

declare(strict_types=1);

namespace Drupdater\Agent\Addon;

use Composer\Composer;

/**
 * An addon whose hooks run here rather than in drupdater: it sees one event at a time and
 * returns what it found and which paths to commit. It never commits, and never calls another.
 */
interface Addon
{
    /**
     * @param array<string, mixed> $event the event's name and its payload, as drupdater sent it
     */
    public function handle(array $event, Composer $composer): HookResult;
}
