<?php

declare(strict_types=1);

namespace Drupdater\App\Pipeline;

use Drupdater\App\Failure;
use Drupdater\App\Json;

/**
 * One addon a run runs: the stage it hooks each event at, and what its hooks found so far.
 */
final class Addon
{
    /**
     * @param array<string, Stage> $hooks by event name
     * @param list<Finding> $findings
     */
    public function __construct(
        public readonly string $name,
        public readonly array $hooks,
        public readonly array $findings = [],
    ) {
    }

    public static function fromJson(mixed $addon): self
    {
        $name = Json::string(Json::at($addon, 'name'));
        if ($name === '') {
            throw new Failure(Failure::INVALID_REQUEST, 'an addon has no name');
        }
        $hooks = [];
        foreach (Json::map(Json::at($addon, 'hooks')) as $event => $stage) {
            if (EventName::tryFrom($event) === null) {
                throw new Failure(Failure::INVALID_REQUEST, sprintf('addon %s hooks the unknown event %s', $name, $event));
            }
            $hooks[$event] = Stage::tryFrom(Json::string($stage))
                ?? throw new Failure(Failure::INVALID_REQUEST, sprintf('addon %s hooks %s at the unknown stage %s', $name, $event, json_encode($stage)));
        }
        return new self($name, $hooks, array_map(Finding::fromJson(...), Json::list(Json::at($addon, 'findings'))));
    }

    public function stage(EventName $event): ?Stage
    {
        return $this->hooks[$event->value] ?? null;
    }

    /**
     * What the addon's hooks found so far, in event and site order.
     *
     * @return list<Finding>
     */
    public function earlier(): array
    {
        $earlier = $this->findings;
        usort($earlier, static fn (Finding $a, Finding $b): int => $a->event->order() <=> $b->event->order() ?: strcmp($a->site, $b->site));
        return $earlier;
    }
}
