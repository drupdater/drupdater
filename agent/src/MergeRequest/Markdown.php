<?php

declare(strict_types=1);

namespace Drupdater\Agent\MergeRequest;

final class Markdown
{
    private const OPERATIONS = [
        'install' => 'New',
        'remove' => 'Removed',
        'upgrade' => 'Upgraded',
        'downgrade' => 'Downgraded',
        'change' => 'Changed',
    ];

    /** Stops a literal "|" or line break in a value from breaking a table. */
    public static function cell(string $value): string
    {
        return strtr($value, ['|' => '\\|', "\n" => ' ', "\r" => '']);
    }

    public static function operation(string $operation): string
    {
        return self::OPERATIONS[$operation] ?? self::cell($operation);
    }

    public static function version(string $version): string
    {
        return $version === '' ? '-' : self::cell($version);
    }
}
