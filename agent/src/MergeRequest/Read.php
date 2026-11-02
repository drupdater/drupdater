<?php

declare(strict_types=1);

namespace Drupdater\Agent\MergeRequest;

use Drupdater\Agent\Failure;

/**
 * Reads a report section as the merge request renders it: a missing or null member is empty, a
 * member of another type is refused, so a section the agent cannot render is never half shown.
 */
final class Read
{
    public static function string(\stdClass $object, string $key, string $section): string
    {
        $value = $object->{$key} ?? '';
        if (!is_string($value)) {
            throw self::unreadable($section, $key . ' must be a string');
        }
        return $value;
    }

    public static function bool(\stdClass $object, string $key, string $section): bool
    {
        $value = $object->{$key} ?? false;
        if (!is_bool($value)) {
            throw self::unreadable($section, $key . ' must be a boolean');
        }
        return $value;
    }

    /**
     * @return list<\stdClass>
     */
    public static function objects(mixed $value, string $section, string $what): array
    {
        $list = self::list($value, $section, $what);
        foreach ($list as $item) {
            if (!$item instanceof \stdClass) {
                throw self::unreadable($section, $what . ' must hold objects');
            }
        }
        /** @var list<\stdClass> $list */
        return $list;
    }

    /**
     * @return list<string>
     */
    public static function strings(mixed $value, string $section, string $what): array
    {
        $list = self::list($value, $section, $what);
        foreach ($list as $item) {
            if (!is_string($item)) {
                throw self::unreadable($section, $what . ' must hold strings');
            }
        }
        /** @var list<string> $list */
        return $list;
    }

    public static function object(mixed $value, string $section, string $what): ?\stdClass
    {
        if ($value !== null && !$value instanceof \stdClass) {
            throw self::unreadable($section, $what . ' must be an object');
        }
        return $value;
    }

    /**
     * @return list<mixed>
     */
    private static function list(mixed $value, string $section, string $what): array
    {
        if ($value === null) {
            return [];
        }
        if (!is_array($value) || !array_is_list($value)) {
            throw self::unreadable($section, $what . ' must be a list');
        }
        return $value;
    }

    private static function unreadable(string $section, string $why): Failure
    {
        return new Failure(Failure::INVALID_REQUEST, sprintf('the report\'s %s section is unreadable: %s', $section, $why));
    }
}
