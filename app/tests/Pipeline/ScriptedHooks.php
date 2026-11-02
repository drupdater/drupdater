<?php

declare(strict_types=1);

namespace Drupdater\App\Tests\Pipeline;

use Drupdater\App\Pipeline\Finding;
use Drupdater\App\Pipeline\HookReply;
use Drupdater\App\Pipeline\Hooks;

/**
 * Answers each addon's hook with what the test scripted for it, and records the calls in order.
 */
final class ScriptedHooks implements Hooks
{
    /** @var list<array{dir: string, addon: string, event: array<string, mixed>, earlier: list<Finding>, security: bool}> */
    public array $calls = [];

    /**
     * @param array<string, \Closure(array<string, mixed>): HookReply> $hooks by addon name
     */
    public function __construct(private readonly array $hooks = [])
    {
    }

    public function run(string $dir, string $addon, array $event, array $earlier, bool $security): HookReply
    {
        $this->calls[] = ['dir' => $dir, 'addon' => $addon, 'event' => $event, 'earlier' => $earlier, 'security' => $security];
        $hook = $this->hooks[$addon] ?? null;
        return $hook === null ? new HookReply() : $hook($event);
    }

    /**
     * @return list<string>
     */
    public function called(): array
    {
        return array_column($this->calls, 'addon');
    }

    /**
     * @return array<string, mixed>
     */
    public function eventOf(string $addon): array
    {
        foreach ($this->calls as $call) {
            if ($call['addon'] === $addon) {
                return $call['event'];
            }
        }
        throw new \LogicException("$addon was not called");
    }
}
