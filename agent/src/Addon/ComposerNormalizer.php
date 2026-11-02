<?php

declare(strict_types=1);

namespace Drupdater\Agent\Addon;

use Drupdater\Agent\Log;
use Drupdater\Agent\Project\Project;
use Drupdater\Agent\Tools\ComposerCli;

/**
 * composer_normalizer: runs ergebnis/composer-normalize where the project has it, so the update's
 * edits to composer.json come out in the project's own format. It reports nothing: what it
 * changes is formatting, not a finding.
 */
final class ComposerNormalizer implements Addon
{
    /**
     * @param \Closure(): Project $project
     * @param \Closure(): ComposerCli $composer built only when the plugin is there to run
     */
    public function __construct(private readonly Log $log, private readonly \Closure $project, private readonly \Closure $composer)
    {
    }

    public static function name(): string
    {
        return 'composer_normalizer';
    }

    /** Format: after every change post-composer-update makes to composer.json. */
    public static function hooks(): array
    {
        return [EventName::PostComposerUpdate->value => Stage::Format];
    }

    public static function create(Services $services): static
    {
        return new self($services->log, $services->project(...), $services->composerCli(...));
    }

    public function handle(Event $event): HookResult
    {
        if (!($this->project)()->installed('ergebnis/composer-normalize')) {
            $this->log->warning('composer-normalize not installed, skipping');
            return new HookResult();
        }
        ($this->composer)()->normalize();
        return new HookResult();
    }

    public static function report(Findings $findings): mixed
    {
        return null;
    }
}
