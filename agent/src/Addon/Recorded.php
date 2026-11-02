<?php

declare(strict_types=1);

namespace Drupdater\Agent\Addon;

/**
 * One hook's finding as drupdater recorded it, with the messages of those of its commits that
 * were made. Kept as JSON text, so an empty object still reads back as one.
 */
final class Recorded
{
    /**
     * @param list<string> $committed
     */
    public function __construct(
        public readonly EventName $event,
        public readonly string $site,
        private readonly string $finding,
        public readonly array $committed,
    ) {
    }

    /**
     * @param bool $associative false keeps JSON objects as \stdClass, where {} and [] differ
     */
    public function finding(bool $associative = true): mixed
    {
        return json_decode($this->finding, $associative, 512, JSON_THROW_ON_ERROR);
    }

    public function committed(string $message): bool
    {
        return in_array($message, $this->committed, true);
    }
}
