<?php

declare(strict_types=1);

namespace Drupdater\App\Pipeline;

use Drupdater\App\Failure;
use Drupdater\App\Json;

/**
 * What one hook of an addon found, with the messages of its commits that were made. The finding
 * itself is the addon's own, passed on as decoded.
 */
final class Finding
{
    /**
     * @param list<string> $committed
     */
    public function __construct(
        public readonly EventName $event,
        public readonly string $site,
        public readonly mixed $finding,
        public readonly array $committed,
    ) {
    }

    public static function fromJson(mixed $finding): self
    {
        $event = EventName::tryFrom(Json::string(Json::at($finding, 'event')));
        if ($event === null) {
            throw new Failure(Failure::INVALID_REQUEST, sprintf('a finding names the unknown event %s', json_encode(Json::at($finding, 'event'))));
        }
        return new self($event, Json::string(Json::at($finding, 'site')), Json::at($finding, 'finding'), Json::strings(Json::at($finding, 'committed')));
    }

    /**
     * @return array{event: string, site: string, finding: mixed, committed: list<string>}
     */
    public function toJson(): array
    {
        return ['event' => $this->event->value, 'site' => $this->site, 'finding' => $this->finding, 'committed' => $this->committed];
    }
}
