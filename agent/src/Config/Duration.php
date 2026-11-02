<?php

declare(strict_types=1);

namespace Drupdater\Agent\Config;

/**
 * Go's time.ParseDuration, which .drupdater.yaml's timeout has always been read with: "30m",
 * "1h30m", "1.5h", "-2s", a bare "0".
 */
final class Duration
{
    private const UNITS = [
        'ns' => 1,
        'us' => 1_000,
        "\u{00B5}s" => 1_000,
        "\u{03BC}s" => 1_000,
        'ms' => 1_000_000,
        's' => 1_000_000_000,
        'm' => 60_000_000_000,
        'h' => 3_600_000_000_000,
    ];

    /**
     * @return int nanoseconds
     * @throws \InvalidArgumentException naming the input, as Go's error does
     */
    public static function parse(string $duration): int
    {
        $invalid = new \InvalidArgumentException(sprintf('time: invalid duration "%s"', $duration));
        $rest = $duration;
        $negative = false;
        if ($rest !== '' && ($rest[0] === '-' || $rest[0] === '+')) {
            $negative = $rest[0] === '-';
            $rest = substr($rest, 1);
        }
        if ($rest === '0') {
            return 0;
        }
        if ($rest === '') {
            throw $invalid;
        }

        $total = 0;
        while ($rest !== '') {
            if (!preg_match('/^(\d*)(?:\.(\d*))?([^\d.]*)/', $rest, $m) || ($m[1] === '' && $m[2] === '')) {
                throw $invalid;
            }
            [$whole, $integer, $fraction, $unit] = [$m[0], $m[1], $m[2], $m[3]];
            if ($unit === '') {
                throw new \InvalidArgumentException(sprintf('time: missing unit in duration "%s"', $duration));
            }
            $scale = self::UNITS[$unit] ?? throw new \InvalidArgumentException(sprintf('time: unknown unit "%s" in duration "%s"', $unit, $duration));

            $value = self::integer($integer === '' ? '0' : $integer) ?? throw $invalid;
            if ($value > intdiv(PHP_INT_MAX, $scale)) {
                throw $invalid;
            }
            $value *= $scale;
            $part = $fraction === '' ? 0 : (int) ((float) ('0.' . $fraction) * $scale);
            if ($value > PHP_INT_MAX - $part) {
                throw $invalid;
            }
            $value += $part;
            if ($total > PHP_INT_MAX - $value) {
                throw $invalid;
            }
            $total += $value;
            $rest = substr($rest, strlen($whole));
        }
        return $negative ? -$total : $total;
    }

    private static function integer(string $digits): ?int
    {
        $digits = ltrim($digits, '0');
        if ($digits === '') {
            return 0;
        }
        if (strlen($digits) > 19 || (strlen($digits) === 19 && strcmp($digits, (string) PHP_INT_MAX) > 0)) {
            return null;
        }
        return (int) $digits;
    }
}
