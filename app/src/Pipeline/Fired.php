<?php

declare(strict_types=1);

namespace Drupdater\App\Pipeline;

/**
 * How firing an event ended. Every outcome carries the findings recorded before it ended, so a
 * failed run still reports what its earlier hooks found.
 */
final class Fired
{
    /**
     * @param list<Recorded> $recorded
     * @param ?UpdatePlan $plan the merged plan; pre-composer-update only
     * @param string $abort why the run has nothing to do, when a hook found it has not
     * @param string $failure why the event failed, when it did
     */
    private function __construct(
        public readonly array $recorded,
        public readonly ?UpdatePlan $plan,
        public readonly string $abort,
        public readonly string $failure,
    ) {
    }

    /**
     * @param list<Recorded> $recorded
     */
    public static function done(array $recorded, ?UpdatePlan $plan): self
    {
        return new self($recorded, $plan, '', '');
    }

    /**
     * @param list<Recorded> $recorded
     */
    public static function aborted(array $recorded, string $reason): self
    {
        return new self($recorded, null, $reason, '');
    }

    /**
     * @param list<Recorded> $recorded
     */
    public static function failed(array $recorded, string $failure): self
    {
        return new self($recorded, null, '', $failure);
    }

    /**
     * @return array{recorded: list<array<string, mixed>>, plan: ?array<string, mixed>, abort: string, failure: string}
     */
    public function toJson(): array
    {
        return [
            'recorded' => array_map(static fn (Recorded $recorded): array => $recorded->toJson(), $this->recorded),
            'plan' => $this->plan?->toJson(),
            'abort' => $this->abort,
            'failure' => $this->failure,
        ];
    }
}
