<?php

declare(strict_types=1);

namespace Drupdater\Agent\Addon;

/**
 * A unit of work that hooks into the run's events. An addon never calls another and never
 * touches git: it returns what it changed as commits and what it found as its finding, and its
 * report section is built from those findings alone.
 */
interface Addon
{
    /** The name .drupdater.yaml uses, and the key of its report section. */
    public static function name(): string;

    /**
     * The events it hooks, each at the stage its hook runs at.
     *
     * @return array<value-of<EventName>, Stage>
     */
    public static function hooks(): array;

    /** Builds the addon from the agent's real tools; a test calls the constructor instead. */
    public static function create(Services $services): static;

    /** Runs its hook for an event hooks() lists. */
    public function handle(Event $event): HookResult;

    /** Its report section, from what its hooks found; null when there is nothing to report. */
    public static function report(Findings $findings): mixed;
}
