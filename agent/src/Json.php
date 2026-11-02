<?php

declare(strict_types=1);

namespace Drupdater\Agent;

/**
 * Reads values drupdater does not control — a tool's JSON report, an HTTP reply, a finding sent
 * back, Composer's config — as the types it expects. A missing or mistyped value reads as empty,
 * never as an error: the tools' own outputs vary between versions, and a field one leaves out
 * means "none".
 */
final class Json
{
    /**
     * The value at path, through arrays and objects alike; null where the path runs out.
     */
    public static function at(mixed $value, string|int ...$path): mixed
    {
        foreach ($path as $key) {
            if (is_array($value)) {
                $value = $value[$key] ?? null;
            }
            elseif ($value instanceof \stdClass) {
                $value = $value->{$key} ?? null;
            }
            else {
                return null;
            }
        }
        return $value;
    }

    /**
     * An object or array as an array keyed by string; anything else as [].
     *
     * @return array<string, mixed>
     */
    public static function map(mixed $value): array
    {
        if ($value instanceof \stdClass) {
            $value = get_object_vars($value);
        }
        if (!is_array($value)) {
            return [];
        }
        $map = [];
        foreach ($value as $key => $item) {
            $map[(string) $key] = $item;
        }
        return $map;
    }

    /**
     * An array's or object's values in order; anything else as [].
     *
     * @return list<mixed>
     */
    public static function list(mixed $value): array
    {
        return array_values(self::map($value));
    }

    /**
     * Each value of list($value) that is itself an array or object, as map() reads it.
     *
     * @return list<array<string, mixed>>
     */
    public static function maps(mixed $value): array
    {
        return array_map(self::map(...), array_values(array_filter(self::list($value), self::isMap(...))));
    }

    /**
     * A scalar as a string; anything else as $default.
     */
    public static function string(mixed $value, string $default = ''): string
    {
        return is_scalar($value) ? (string) $value : $default;
    }

    /**
     * A number or numeric string as an int; anything else as 0.
     */
    public static function int(mixed $value): int
    {
        return is_numeric($value) ? (int) $value : 0;
    }

    /**
     * Each scalar of list($value) as a string.
     *
     * @return list<string>
     */
    public static function strings(mixed $value): array
    {
        return array_map(self::string(...), array_values(array_filter(self::list($value), is_scalar(...))));
    }

    private static function isMap(mixed $value): bool
    {
        return is_array($value) || $value instanceof \stdClass;
    }
}
