<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\Config;

use Drupdater\Agent\Addon\Addon;
use Drupdater\Agent\Addon\Event;
use Drupdater\Agent\Addon\Findings;
use Drupdater\Agent\Addon\HookResult;
use Drupdater\Agent\Addon\Services;

/**
 * An addon that is only a name in a catalogue, so a test can order two of them.
 */
final class NamedAddon implements Addon
{
    public static function name(): string
    {
        return 'named';
    }

    public static function hooks(): array
    {
        return [];
    }

    public static function create(Services $services): static
    {
        return new self();
    }

    public function handle(Event $event): HookResult
    {
        throw new \LogicException('never run');
    }

    public static function report(Findings $findings): mixed
    {
        return null;
    }
}
