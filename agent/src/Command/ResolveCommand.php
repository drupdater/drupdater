<?php

declare(strict_types=1);

namespace Drupdater\Agent\Command;

use Drupdater\Agent\Addon\Pin;
use Drupdater\Agent\Failure;
use Drupdater\Agent\Resolve\Resolver;

/**
 * Resolves the run's update into composer.lock and reports what it changed; see Resolver.
 *
 * Request: {packages[], keep[{package, version}], minimal_changes}. Result: {changes[]}.
 */
final class ResolveCommand extends AgentCommand
{
    protected function configure(): void
    {
        $this->setName('drupdater:resolve')
            ->setDescription('Resolves an update into composer.lock and reports what changed.')
            ->setHidden(true);
    }

    protected function handle(array $request): array
    {
        $packages = self::strings($request, 'packages');
        $keep = $request['keep'] ?? [];
        if (!is_array($keep) || !array_is_list($keep)) {
            throw new Failure(Failure::INVALID_REQUEST, 'keep must be a list');
        }
        $pins = array_map(self::pin(...), $keep);
        return ['changes' => $this->resolver()->resolve($packages, $pins, ($request['minimal_changes'] ?? false) === true, true)];
    }

    private static function pin(mixed $pin): Pin
    {
        if (!is_array($pin) || !is_string($pin['package'] ?? null) || !is_string($pin['version'] ?? null)
            || $pin['package'] === '' || $pin['version'] === '') {
            throw new Failure(Failure::INVALID_REQUEST, 'each keep must have a package and a version');
        }
        return new Pin($pin['package'], $pin['version']);
    }

    /**
     * @param array<string, mixed> $request
     * @return list<string>
     */
    private static function strings(array $request, string $key): array
    {
        $value = $request[$key] ?? [];
        if (!is_array($value) || !array_is_list($value) || array_filter($value, 'is_string') !== $value) {
            throw new Failure(Failure::INVALID_REQUEST, sprintf('%s must be a list of strings', $key));
        }
        return $value;
    }
}
