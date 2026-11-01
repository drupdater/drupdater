<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\Addon;

use Drupdater\Agent\Addon\Addon;
use Drupdater\Agent\Addon\Event;
use Drupdater\Agent\Addon\EventName;
use Drupdater\Agent\Addon\Findings;
use Drupdater\Agent\Addon\HookResult;
use Drupdater\Agent\Addon\Services;
use Drupdater\Agent\Addon\Stage;

/**
 * An addon whose hook runs whatever the test set: for the commands' own behaviour, apart from
 * any real addon's.
 */
final class ScriptedAddon implements Addon
{
    /** @var (\Closure(Event, Services): HookResult)|null */
    public static ?\Closure $hook = null;

    /** @var (\Closure(Findings): mixed)|null */
    public static ?\Closure $report = null;

    private function __construct(private readonly Services $services)
    {
    }

    public static function name(): string
    {
        return 'scripted';
    }

    public static function hooks(): array
    {
        return [EventName::PreComposerUpdate->value => Stage::Change, EventName::PostCodeUpdate->value => Stage::Inspect];
    }

    public static function create(Services $services): static
    {
        return new self($services);
    }

    public function handle(Event $event): HookResult
    {
        return (self::$hook ?? throw new \LogicException('no hook scripted'))($event, $this->services);
    }

    public static function report(Findings $findings): mixed
    {
        return (self::$report ?? static fn (): mixed => null)($findings);
    }
}
