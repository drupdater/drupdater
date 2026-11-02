<?php

declare(strict_types=1);

namespace Drupdater\Agent\Config;

/**
 * One mapping of the parsed file, read key by key. A null value reads as an absent key, except
 * where the caller says otherwise, as the binary's YAML decoding did.
 *
 * @internal
 */
final class File
{
    /**
     * @param array<mixed> $mapping
     */
    public function __construct(private readonly array $mapping, private readonly string $at = '')
    {
    }

    public function has(string $key): bool
    {
        return array_key_exists($key, $this->mapping);
    }

    /** Present with a value: a key left blank is null, which reads as absent. */
    public function given(string $key): bool
    {
        return isset($this->mapping[$key]);
    }

    /**
     * A scalar as written, for a value such as a duration that is parsed later. Null keeps the default.
     *
     * @throws \UnexpectedValueException
     */
    public function scalar(string $key): ?string
    {
        $value = $this->mapping[$key] ?? null;
        return match (true) {
            $value === null => null,
            is_string($value) => $value,
            is_int($value) => (string) $value,
            is_bool($value) => $value ? 'true' : 'false',
            is_float($value) => self::float($value),
            default => throw $this->unexpected($key, 'a single value'),
        };
    }

    /**
     * A list of names; null reads as empty, so a key left blank names none.
     *
     * @return list<string>
     * @throws \UnexpectedValueException
     */
    public function strings(string $key): array
    {
        $value = $this->mapping[$key] ?? [];
        if (!is_array($value) || !array_is_list($value)) {
            throw $this->unexpected($key, 'a list');
        }
        $strings = [];
        foreach ($value as $item) {
            if (is_int($item)) {
                $item = (string) $item;
            }
            if (!is_string($item)) {
                throw $this->unexpected($key, 'a list of names');
            }
            $strings[] = $item;
        }
        return $strings;
    }

    /**
     * @param array<string, bool> $booleans what else reads as true or false
     * @throws \UnexpectedValueException
     */
    public function bool(string $key, array $booleans): bool
    {
        $value = $this->mapping[$key] ?? false;
        if (is_string($value) && array_key_exists($value, $booleans)) {
            return $booleans[$value];
        }
        if (!is_bool($value)) {
            throw $this->unexpected($key, 'true or false');
        }
        return $value;
    }

    /**
     * The mapping at $key, then $name under it, each empty when absent. Once read, $key's own
     * keys are checked against the ones its blocks may have.
     *
     * @throws \UnexpectedValueException
     */
    public function block(string $key, string $name): self
    {
        $outer = $this->mapping($key);
        $outer->refuseUnknown(ProjectConfig::RUN_TYPES);
        $inner = $outer->mapping($name);
        $inner->refuseUnknown(['addons', 'auto_merge']);
        return $inner;
    }

    /**
     * @param list<string> $known
     * @throws \UnexpectedValueException naming the first key of the file that is not one of $known
     */
    public function refuseUnknown(array $known): void
    {
        foreach (array_keys($this->mapping) as $key) {
            if (!in_array($key, $known, true)) {
                throw new \UnexpectedValueException(sprintf('unknown key "%s"', $this->at . $key));
            }
        }
    }

    private function mapping(string $key): self
    {
        $value = $this->mapping[$key] ?? [];
        if (!is_array($value) || (array_is_list($value) && $value !== [])) {
            throw $this->unexpected($key, 'a mapping of keys');
        }
        return new self($value, $this->at . $key . '.');
    }

    private function unexpected(string $key, string $expected): \UnexpectedValueException
    {
        return new \UnexpectedValueException(sprintf('"%s" must be %s', $this->at . $key, $expected));
    }

    /** As the file spells it, near enough: 1.5 stays 1.5, 1.0 stays 1.0. */
    private static function float(float $value): string
    {
        $text = (string) $value;
        return str_contains($text, '.') || !is_finite($value) ? $text : $text . '.0';
    }
}
