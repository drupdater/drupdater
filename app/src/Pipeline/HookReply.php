<?php

declare(strict_types=1);

namespace Drupdater\App\Pipeline;

use Drupdater\App\Json;

/**
 * What a hook returned: its finding, the commits it asks for, its proposal for the update, and
 * why the run has nothing to do, when it found it has not.
 */
final class HookReply
{
    /**
     * @param list<Commit> $commits
     */
    public function __construct(
        public readonly mixed $finding = null,
        public readonly array $commits = [],
        public readonly ?UpdatePlan $plan = null,
        public readonly string $abort = '',
    ) {
    }

    public static function fromJson(\stdClass $reply): self
    {
        $plan = Json::at($reply, 'plan');
        return new self(
            Json::at($reply, 'finding'),
            array_map(Commit::fromJson(...), Json::list(Json::at($reply, 'commits'))),
            $plan === null ? null : UpdatePlan::fromJson($plan),
            Json::string(Json::at($reply, 'abort')),
        );
    }
}
