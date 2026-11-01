<?php

declare(strict_types=1);

namespace Drupdater\Agent\Command;

use Composer\Factory;
use Composer\Policy\PolicyConfig;
use Drupdater\Agent\Addon\AbortRun;
use Drupdater\Agent\Addon\Catalogue;
use Drupdater\Agent\Addon\Event;
use Drupdater\Agent\Addon\EventName;
use Drupdater\Agent\Addon\Findings;
use Drupdater\Agent\Addon\Services;
use Drupdater\Agent\Failure;
use Drupdater\Agent\Log;

/**
 * Runs one addon's hook for one event. drupdater calls it once per hook, in stage order, and
 * commits what each asks for before calling the next.
 *
 * Request: {addon, event{name, ...payload}, earlier[{event, site, finding, committed[]}], run{security}}.
 * Result: {finding, commits[{message, paths[]}], plan, abort}: plan only on pre-composer-update,
 * abort the reason the run has nothing to do, when it has not.
 */
final class HookCommand extends AgentCommand
{
    private readonly Catalogue $catalogue;

    public function __construct(?Catalogue $catalogue = null)
    {
        $this->catalogue = $catalogue ?? Catalogue::builtin();
        parent::__construct();
    }

    protected function configure(): void
    {
        $this->setName('drupdater:hook')
            ->setDescription('Runs one addon\'s hook for one workflow event.')
            ->setHidden(true);
    }

    protected function handle(array $request): array
    {
        $name = $request['addon'] ?? null;
        $entry = is_string($name) ? $this->catalogue->find($name) : null;
        if ($entry === null) {
            throw new Failure(Failure::INVALID_REQUEST, sprintf('no addon %s is in the catalogue', json_encode($name)));
        }
        $this->log = new Log(['addon' => $entry->name()]);

        $payload = $request['event'] ?? null;
        if (!is_array($payload)) {
            throw new Failure(Failure::INVALID_REQUEST, 'event must be an object');
        }
        $security = $request['run']['security'] ?? false;
        if (!is_bool($security)) {
            throw new Failure(Failure::INVALID_REQUEST, 'run.security must be a boolean');
        }
        $composerFile = Factory::getComposerFile();
        $event = Event::fromRequest($payload, dirname((string) (realpath($composerFile) ?: $composerFile)), $security, Findings::fromJson($this->rawMember('earlier')));
        $addon = $entry->addon;
        if (!array_key_exists($event->name->value, $addon::hooks())) {
            throw new Failure(Failure::INVALID_REQUEST, sprintf('%s has no hook for %s', $entry->name(), $event->name->value));
        }

        $composer = $this->requireComposer();
        $services = new Services(
            $composer,
            $this->getIO(),
            $this->resolver(),
            fn (): PolicyConfig => $this->createPolicyConfig($composer->getConfig(), null),
            $this->log,
        );
        try {
            $result = $addon::create($services)->handle($event);
        }
        catch (AbortRun $abort) {
            return ['finding' => null, 'commits' => [], 'plan' => null, 'abort' => $abort->getMessage()];
        }
        if ($result->plan !== null && $event->name !== EventName::PreComposerUpdate) {
            throw new \LogicException(sprintf('%s proposed a plan on %s, where nothing reads it', $entry->name(), $event->name->value));
        }
        return $result->toArray() + ['abort' => null];
    }
}
