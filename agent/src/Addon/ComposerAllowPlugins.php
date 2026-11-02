<?php

declare(strict_types=1);

namespace Drupdater\Agent\Addon;

use Drupdater\Agent\Failure;
use Drupdater\Agent\Plugins\AllowPlugins;

/**
 * composer_allow_plugins: lets an unattended update install plugins Composer would otherwise
 * block, then records each new one as denied, for a human to decide on.
 */
final class ComposerAllowPlugins implements Addon
{
    public function __construct(private readonly AllowPlugins $plugins)
    {
    }

    public static function name(): string
    {
        return 'composer_allow_plugins';
    }

    public static function hooks(): array
    {
        return [
            EventName::PreComposerUpdate->value => Stage::Change,
            EventName::PostComposerUpdate->value => Stage::Change,
        ];
    }

    public static function create(Services $services): static
    {
        return new self($services->allowPlugins());
    }

    /**
     * Opens allow-plugins before the update and proposes nothing about the update itself; closes
     * it after, so composer.json ends as it began plus one denial per new plugin.
     */
    public function handle(Event $event): HookResult
    {
        if ($event->name === EventName::PreComposerUpdate) {
            return new HookResult($this->plugins->open());
        }
        $opened = $event->earlier->shared(EventName::PreComposerUpdate)?->finding(false);
        if (!$opened instanceof \stdClass) {
            // Closing on a guess would deny every plugin the project's own rules allow.
            throw new Failure(Failure::INVALID_REQUEST, 'composer_allow_plugins closes only what it opened, and its opening was not sent');
        }
        $original = $opened->original ?? null;
        if ($original !== null && !$original instanceof \stdClass && !is_bool($original)) {
            throw new \LogicException('allow-plugins was recorded as neither an object, a boolean nor absent');
        }
        return new HookResult($this->plugins->close($original, ($opened->opened ?? false) === true));
    }

    /** The plugins it denied, which each need someone to decide whether to trust them. */
    public static function report(Findings $findings): mixed
    {
        $denied = $findings->shared(EventName::PostComposerUpdate)?->finding();
        if (!is_array($denied) || $denied === []) {
            return null;
        }
        sort($denied, SORT_STRING);
        return ['added' => array_values($denied)];
    }
}
