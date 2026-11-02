<?php

declare(strict_types=1);

namespace Drupdater\Agent\Addon;

use Drupdater\Agent\Failure;

/**
 * What `composer update` is asked to do. On pre-composer-update a hook reads the plan earlier
 * stages settled and proposes its own; drupdater merges the proposals.
 */
final class Plan
{
    /**
     * @param list<string> $only restricts the update to these packages; empty means every package
     * @param list<Pin> $keep
     */
    public function __construct(
        public readonly array $only = [],
        public readonly array $keep = [],
        public readonly bool $minimalChanges = false,
    ) {
    }

    /**
     * @throws Failure when the plan is malformed
     */
    public static function fromArray(mixed $plan): self
    {
        if (!is_array($plan)) {
            throw new Failure(Failure::INVALID_REQUEST, 'plan must be an object');
        }
        $only = $plan['only'] ?? [];
        if (!is_array($only) || !array_is_list($only) || array_filter($only, 'is_string') !== $only) {
            throw new Failure(Failure::INVALID_REQUEST, 'plan.only must be a list of strings');
        }
        $keep = $plan['keep'] ?? [];
        if (!is_array($keep) || !array_is_list($keep)) {
            throw new Failure(Failure::INVALID_REQUEST, 'plan.keep must be a list');
        }
        $pins = [];
        foreach ($keep as $pin) {
            if (!is_array($pin) || !is_string($pin['package'] ?? null) || !is_string($pin['version'] ?? null)) {
                throw new Failure(Failure::INVALID_REQUEST, 'each plan.keep must have a package and a version');
            }
            $pins[] = new Pin($pin['package'], $pin['version']);
        }
        return new self($only, $pins, ($plan['minimal_changes'] ?? false) === true);
    }

    /**
     * @return array{only: list<string>, keep: list<array{package: string, version: string}>, minimal_changes: bool}
     */
    public function toArray(): array
    {
        return [
            'only' => $this->only,
            'keep' => array_map(static fn (Pin $pin): array => $pin->toArray(), $this->keep),
            'minimal_changes' => $this->minimalChanges,
        ];
    }
}
