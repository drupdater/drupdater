<?php

declare(strict_types=1);

namespace Drupdater\Agent\Addon;

/**
 * One addon of the catalogue, with whether every run needs it and whether a normal run that
 * names no addons gets it.
 */
final class CatalogueEntry
{
    /**
     * @param class-string<Addon> $addon
     */
    public function __construct(
        public readonly string $addon,
        public readonly bool $mandatory = false,
        public readonly bool $enabledByDefault = false,
    ) {
        if ($mandatory && $enabledByDefault) {
            throw new \LogicException(sprintf('%s is mandatory, so it is not a default', $addon));
        }
    }

    public function name(): string
    {
        return $this->addon::name();
    }

    /**
     * @return array{name: string, mandatory: bool, default: bool, hooks: array<string, string>}
     */
    public function toArray(): array
    {
        return [
            'name' => $this->name(),
            'mandatory' => $this->mandatory,
            'default' => $this->enabledByDefault,
            'hooks' => array_map(static fn (Stage $stage): string => $stage->value, $this->addon::hooks()),
        ];
    }
}
