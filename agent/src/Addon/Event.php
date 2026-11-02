<?php

declare(strict_types=1);

namespace Drupdater\Agent\Addon;

use Drupdater\Agent\Failure;

/**
 * One event as a hook sees it: its name, what that event carries, and what the addon's own
 * hooks found earlier in the run.
 */
final class Event
{
    /**
     * @param array<mixed>|null $baseLock
     */
    private function __construct(
        public readonly EventName $name,
        public readonly string $projectDir,
        public readonly bool $security,
        public readonly Findings $earlier,
        private readonly ?Plan $plan = null,
        private readonly ?array $baseLock = null,
        private readonly ?string $site = null,
        private readonly ?SiteFacts $facts = null,
    ) {
    }

    /**
     * @param array<mixed> $event the request's event: its name and that event's payload
     *
     * @throws Failure when the event is unknown or lacks its payload
     */
    public static function fromRequest(array $event, string $projectDir, bool $security, Findings $earlier): self
    {
        $name = is_string($event['name'] ?? null) ? EventName::tryFrom($event['name']) : null;
        return match ($name) {
            EventName::PreComposerUpdate => new self($name, $projectDir, $security, $earlier, plan: Plan::fromArray($event['plan'] ?? null)),
            EventName::PostComposerUpdate => new self($name, $projectDir, $security, $earlier, baseLock: self::readBaseLock($event)),
            EventName::PostCodeUpdate => new self($name, $projectDir, $security, $earlier),
            EventName::PreSiteUpdate, EventName::PostSiteUpdate => new self(
                $name, $projectDir, $security, $earlier, site: self::readSite($event), facts: SiteFacts::fromArray($event['facts'] ?? null),
            ),
            null => throw new Failure(Failure::INVALID_REQUEST, sprintf('unknown event %s', json_encode($event['name'] ?? null))),
        };
    }

    public static function preComposerUpdate(string $projectDir, Plan $plan, bool $security = false, Findings $earlier = new Findings()): self
    {
        return new self(EventName::PreComposerUpdate, $projectDir, $security, $earlier, plan: $plan);
    }

    /**
     * @param array<mixed> $baseLock
     */
    public static function postComposerUpdate(string $projectDir, array $baseLock, Findings $earlier = new Findings()): self
    {
        return new self(EventName::PostComposerUpdate, $projectDir, false, $earlier, baseLock: $baseLock);
    }

    public static function postCodeUpdate(string $projectDir, bool $security = false, Findings $earlier = new Findings()): self
    {
        return new self(EventName::PostCodeUpdate, $projectDir, $security, $earlier);
    }

    public static function siteUpdate(EventName $name, string $projectDir, string $site, SiteFacts $facts, Findings $earlier = new Findings()): self
    {
        if (!$name->perSite()) {
            throw new \LogicException(sprintf('%s is not a per-site event', $name->value));
        }
        return new self($name, $projectDir, false, $earlier, site: $site, facts: $facts);
    }

    /** The update as the event's earlier stages settled it. Pre-composer-update only. */
    public function plan(): Plan
    {
        return $this->plan ?? throw new \LogicException(sprintf('%s carries no plan', $this->name->value));
    }

    /**
     * composer.lock as committed when the event fired: the lock the update started from.
     * Post-composer-update only.
     *
     * @return array<mixed>
     */
    public function baseLock(): array
    {
        return $this->baseLock ?? throw new \LogicException(sprintf('%s carries no base lock', $this->name->value));
    }

    public function site(): string
    {
        return $this->site ?? throw new \LogicException(sprintf('%s is not per site', $this->name->value));
    }

    /** The site's facts: before its update hooks ran on pre-site-update, after on post-site-update. */
    public function facts(): SiteFacts
    {
        return $this->facts ?? throw new \LogicException(sprintf('%s is not per site', $this->name->value));
    }

    /**
     * @param array<mixed> $event
     * @return array<mixed>
     */
    private static function readBaseLock(array $event): array
    {
        $lock = $event['base_lock'] ?? null;
        if (!is_array($lock)) {
            throw new Failure(Failure::INVALID_REQUEST, 'base_lock must be the composer.lock the update started from');
        }
        return $lock;
    }

    /**
     * @param array<mixed> $event
     */
    private static function readSite(array $event): string
    {
        $site = $event['site'] ?? null;
        if (!is_string($site) || $site === '') {
            throw new Failure(Failure::INVALID_REQUEST, 'a per-site event must name its site');
        }
        return $site;
    }
}
