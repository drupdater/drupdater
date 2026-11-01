<?php

declare(strict_types=1);

namespace Drupdater\Agent\Command;

use Drupdater\Agent\Addon\Addon;
use Drupdater\Agent\Addon\ComposerDiff;
use Drupdater\Agent\Failure;

/**
 * Runs one addon's hook for one event, for the addons that live here rather than in drupdater.
 *
 * Request: {addon, event: {name, ...payload}}. Result: {commits[{message, paths[]}], finding, log[]};
 * see HookResult.
 */
final class AddonCommand extends AgentCommand
{
    /**
     * @param array<string, Addon> $addons by the name .drupdater.yaml uses
     */
    public function __construct(private readonly array $addons = [ComposerDiff::NAME => new ComposerDiff()])
    {
        parent::__construct();
    }

    protected function configure(): void
    {
        $this->setName('drupdater:addon')
            ->setDescription('Runs one addon\'s hook for one workflow event.')
            ->setHidden(true);
    }

    protected function handle(array $request): array
    {
        $name = $request['addon'] ?? null;
        $addon = is_string($name) ? ($this->addons[$name] ?? null) : null;
        if ($addon === null) {
            throw new Failure(Failure::INVALID_REQUEST, sprintf('no addon %s runs in the agent', json_encode($name)));
        }
        $event = $request['event'] ?? null;
        if (!is_array($event)) {
            throw new Failure(Failure::INVALID_REQUEST, 'event must be an object');
        }
        return $addon->handle($event, $this->requireComposer())->toArray();
    }
}
