<?php

declare(strict_types=1);

namespace Drupdater\App\Pipeline;

use Drupdater\App\Failure;
use Drupdater\App\Json;

/**
 * One point of a run the addons hook into, and what it hands their hooks.
 */
final class Event
{
    /**
     * @param string $site the site of a per-site event, '' otherwise
     * @param UpdatePlan $plan the update as the run starts to settle it; pre-composer-update only
     * @param mixed $baseLock composer.lock as committed when the event fired; post-composer-update only
     * @param mixed $facts the site's facts as the agent read them; per-site events only
     */
    public function __construct(
        public readonly EventName $name,
        public readonly string $site = '',
        public readonly UpdatePlan $plan = new UpdatePlan(),
        public readonly mixed $baseLock = null,
        public readonly mixed $facts = null,
    ) {
    }

    public static function fromJson(mixed $event): self
    {
        $name = EventName::tryFrom(Json::string(Json::at($event, 'name')))
            ?? throw new Failure(Failure::INVALID_REQUEST, sprintf('unknown event %s', json_encode(Json::at($event, 'name'))));
        return new self(
            $name,
            Json::string(Json::at($event, 'site')),
            UpdatePlan::fromJson(Json::at($event, 'plan')),
            Json::at($event, 'base_lock'),
            Json::at($event, 'facts'),
        );
    }

    /**
     * The event as a hook sees it, with the plan its stage settled.
     *
     * @return array<string, mixed>
     */
    public function payload(UpdatePlan $settled): array
    {
        $payload = ['name' => $this->name->value];
        if ($this->site !== '') {
            $payload['site'] = $this->site;
        }
        if ($this->name === EventName::PreComposerUpdate) {
            $payload['plan'] = $settled->toJson();
        }
        if ($this->baseLock !== null) {
            $payload['base_lock'] = $this->baseLock;
        }
        if ($this->facts !== null) {
            $payload['facts'] = $this->facts;
        }
        return $payload;
    }
}
