<?php

declare(strict_types=1);

namespace Drupdater\App\Pipeline;

use Drupdater\App\Agent\AgentError;
use Drupdater\App\Git\CommitFailed;
use Drupdater\App\Git\Committer;

/**
 * Runs a set of addons' hooks for one event: in stage order, each in the agent, committing what
 * each asks for before the next runs. It stops at the first failing hook, so a later one never
 * sees a half-done event.
 */
final class Pipeline
{
    public function __construct(private readonly Hooks $hooks, private readonly Committer $committer)
    {
    }

    /**
     * Fires event at addons, in this order within a stage, in the project in dir. On
     * pre-composer-update the outcome carries the event's plan merged with every hook's proposal;
     * each hook sees the plan as earlier stages settled it, so hooks sharing a stage cannot see
     * each other's proposals.
     *
     * @param list<Addon> $addons
     */
    public function fire(string $dir, Event $event, array $addons, bool $security): Fired
    {
        $failed = sprintf('%s hook failed', $event->name->value);
        $plan = $event->plan;
        $settled = $plan;
        $stage = null;
        $recorded = [];
        foreach (self::ordered($event->name, $addons) as [$addon, $hookStage]) {
            if ($hookStage !== $stage) {
                $settled = $plan;
                $stage = $hookStage;
            }
            try {
                $reply = $this->hooks->run($dir, $addon->name, $event->payload($settled), $addon->earlier(), $security);
            }
            catch (AgentError $e) {
                return Fired::failed($recorded, sprintf('%s: %s: %s', $failed, $addon->name, $e->getMessage()));
            }
            if ($reply->abort !== '') {
                return Fired::aborted($recorded, $reply->abort);
            }
            if ($reply->plan !== null && $event->name !== EventName::PreComposerUpdate) {
                return Fired::failed($recorded, sprintf('%s: %s proposed a plan, which only pre-composer-update reads', $failed, $addon->name));
            }
            try {
                $committed = $this->commitAll($reply->commits);
            }
            catch (CommitFailed $e) {
                return Fired::failed($recorded, sprintf('%s: %s: %s', $failed, $addon->name, $e->getMessage()));
            }
            $recorded[] = new Recorded($addon->name, new Finding($event->name, $event->site, $reply->finding, $committed));
            if ($reply->plan === null) {
                continue;
            }
            try {
                $plan = $plan->merge($reply->plan);
            }
            catch (PlanConflict $e) {
                return Fired::failed($recorded, sprintf('%s hooks disagree: %s', $event->name->value, $e->getMessage()));
            }
        }
        return Fired::done($recorded, $event->name === EventName::PreComposerUpdate ? $plan : null);
    }

    /**
     * The hooks of event by stage, hooks sharing a stage in addon order.
     *
     * @param list<Addon> $addons
     * @return list<array{Addon, Stage}>
     */
    private static function ordered(EventName $event, array $addons): array
    {
        $hooks = [];
        foreach ($addons as $addon) {
            $stage = $addon->stage($event);
            if ($stage !== null) {
                $hooks[] = [$addon, $stage];
            }
        }
        // usort is stable, so addon order survives within a stage.
        usort($hooks, static fn (array $a, array $b): int => $a[1]->order() <=> $b[1]->order());
        return $hooks;
    }

    /**
     * Makes a hook's commits in order, returning the messages of those that were made.
     *
     * @param list<Commit> $commits
     * @return list<string>
     */
    private function commitAll(array $commits): array
    {
        $committed = [];
        foreach ($commits as $commit) {
            if ($this->committer->commit($commit->message, $commit->paths)) {
                $committed[] = $commit->message;
            }
        }
        return $committed;
    }
}
