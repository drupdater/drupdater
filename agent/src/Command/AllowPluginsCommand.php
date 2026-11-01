<?php

declare(strict_types=1);

namespace Drupdater\Agent\Command;

use Composer\Factory;
use Drupdater\Agent\Failure;
use Drupdater\Agent\Plugins\AllowPlugins;

/**
 * Opens allow-plugins for an update and closes it afterwards; see AllowPlugins.
 *
 * Request: {step: "open"}, result {original, opened}: allow-plugins as written (null when
 * absent) and whether a wildcard was added. Request: {step: "close", original, opened} with that
 * result, result {denied[]}.
 */
final class AllowPluginsCommand extends AgentCommand
{
    protected function configure(): void
    {
        $this->setName('drupdater:allow-plugins')
            ->setDescription('Opens allow-plugins for an update, and closes it again afterwards.')
            ->setHidden(true);
    }

    protected function handle(array $request): array
    {
        $plugins = new AllowPlugins($this->requireComposer(), (string) realpath(Factory::getComposerFile()));
        return match ($request['step'] ?? null) {
            'open' => $plugins->open(),
            'close' => ['denied' => $plugins->close($this->original(), self::opened($request))],
            default => throw new Failure(Failure::INVALID_REQUEST, 'step must be "open" or "close"'),
        };
    }

    private function original(): \stdClass|bool|null
    {
        $original = $this->rawMember('original');
        if ($original !== null && !$original instanceof \stdClass && !is_bool($original)) {
            throw new Failure(Failure::INVALID_REQUEST, 'original must be an object, a boolean or null');
        }
        return $original;
    }

    /**
     * @param array<string, mixed> $request
     */
    private static function opened(array $request): bool
    {
        if (!is_bool($request['opened'] ?? null)) {
            throw new Failure(Failure::INVALID_REQUEST, 'opened must be a boolean');
        }
        return $request['opened'];
    }
}
