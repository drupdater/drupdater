<?php

declare(strict_types=1);

namespace Drupdater\App\Command;

use Drupdater\App\Failure;
use Drupdater\App\Git\Committer;
use Drupdater\App\Git\Identity;
use Drupdater\App\Json;
use Drupdater\App\Log;
use Drupdater\App\Pipeline\Addon;
use Drupdater\App\Pipeline\Event;
use Drupdater\App\Pipeline\Hooks;
use Drupdater\App\Pipeline\Pipeline;

/**
 * Fires one event at the run's addons.
 *
 * Request: {event{name, site, plan, base_lock, facts}, addons[{name, hooks{event: stage},
 * findings[]}], run{security}, author{name, email}}. Result: {recorded[{addon, finding}], plan,
 * abort, failure}: an event that aborted or failed still answers, with what it recorded first.
 */
final class FireCommand extends EnvelopeCommand
{
    /**
     * @param \Closure(string, Identity): Committer $committer the committer into dir's index
     */
    public function __construct(Log $log, private readonly Hooks $hooks, private readonly \Closure $committer)
    {
        parent::__construct($log);
    }

    protected function configure(): void
    {
        $this->setName('internal:fire')
            ->setDescription('Fires one workflow event at the run\'s addons.')
            ->setHidden(true);
    }

    protected function handle(string $dir, \stdClass $request): array
    {
        $security = Json::at($request, 'run', 'security') ?? false;
        if (!is_bool($security)) {
            throw new Failure(Failure::INVALID_REQUEST, 'run.security must be a boolean');
        }
        $pipeline = new Pipeline($this->hooks, ($this->committer)($dir, Author::fromJson(Json::at($request, 'author'))));
        return $pipeline->fire(
            $dir,
            Event::fromJson(Json::at($request, 'event')),
            array_map(Addon::fromJson(...), Json::list(Json::at($request, 'addons'))),
            $security,
        )->toJson();
    }
}
