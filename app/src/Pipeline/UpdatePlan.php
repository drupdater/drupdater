<?php

declare(strict_types=1);

namespace Drupdater\App\Pipeline;

use Drupdater\App\Json;

/**
 * What `composer update` is asked to do. A pre-composer-update hook reads the plan earlier stages
 * settled and returns its own proposal; the pipeline merges the proposals.
 */
final class UpdatePlan
{
    /**
     * @param list<string> $only restricts the update to these packages; empty means every package
     * @param list<Pin> $keep holds packages at a version, whatever else the update does
     * @param bool $minimalChanges asks Composer to disturb as little else as possible
     */
    public function __construct(
        public readonly array $only = [],
        public readonly array $keep = [],
        public readonly bool $minimalChanges = false,
    ) {
    }

    public static function fromJson(mixed $plan): self
    {
        return new self(
            Json::strings(Json::at($plan, 'only')),
            array_map(
                static fn (array $pin): Pin => new Pin(Json::string($pin['package'] ?? null), Json::string($pin['version'] ?? null)),
                Json::maps(Json::at($plan, 'keep')),
            ),
            Json::at($plan, 'minimal_changes') === true,
        );
    }

    /**
     * Commutative and associative, so the order addons propose in never changes the plan:
     * restrictions intersect, pins accumulate, and any proposal can ask for minimal changes.
     *
     * @throws PlanConflict
     */
    public function merge(self $other): self
    {
        return new self(
            self::intersect($this->only, $other->only),
            self::union($this->keep, $other->keep),
            $this->minimalChanges || $other->minimalChanges,
        );
    }

    /**
     * @return array{only: list<string>, keep: list<array{package: string, version: string}>, minimal_changes: bool}
     */
    public function toJson(): array
    {
        return [
            'only' => $this->only,
            'keep' => array_map(static fn (Pin $pin): array => $pin->toJson(), $this->keep),
            'minimal_changes' => $this->minimalChanges,
        ];
    }

    /**
     * An empty intersection is refused: passed on, it would read as "update everything", the
     * opposite of what both proposals asked for.
     *
     * @param list<string> $a
     * @param list<string> $b
     * @return list<string>
     */
    private static function intersect(array $a, array $b): array
    {
        $a = self::sortedSet($a);
        $b = self::sortedSet($b);
        if ($a === []) {
            return $b;
        }
        if ($b === []) {
            return $a;
        }
        $both = array_values(array_intersect($a, $b));
        if ($both === []) {
            throw new PlanConflict(sprintf('the update is restricted to disjoint package sets: %s and %s', implode(', ', $a), implode(', ', $b)));
        }
        return $both;
    }

    /**
     * @param list<Pin> $a
     * @param list<Pin> $b
     * @return list<Pin>
     */
    private static function union(array $a, array $b): array
    {
        $pins = [...$a, ...$b];
        usort($pins, static fn (Pin $x, Pin $y): int => strcmp($x->package, $y->package) ?: strcmp($x->version, $y->version));
        $union = [];
        foreach ($pins as $pin) {
            $last = end($union);
            if ($last === false || $last->package !== $pin->package) {
                $union[] = $pin;
                continue;
            }
            if ($last->version !== $pin->version) {
                throw new PlanConflict(sprintf('%s is pinned at both %s and %s', $pin->package, $last->version, $pin->version));
            }
        }
        return $union;
    }

    /**
     * @param list<string> $packages
     * @return list<string>
     */
    private static function sortedSet(array $packages): array
    {
        $packages = array_values(array_unique($packages));
        sort($packages, SORT_STRING);
        return $packages;
    }
}
