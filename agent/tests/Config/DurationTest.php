<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\Config;

use Drupdater\Agent\Config\Duration;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;

/**
 * Every case was checked against Go's time.ParseDuration, which the timeout was read with before.
 */
final class DurationTest extends TestCase
{
    /**
     * @return iterable<string, array{string, int}>
     */
    public static function durations(): iterable
    {
        yield '30m' => ['30m', 1_800_000_000_000];
        yield 'a bare zero' => ['0', 0];
        yield 'a signed zero' => ['+0', 0];
        yield 'a negative zero' => ['-0', 0];
        yield 'two units' => ['1h30m', 5_400_000_000_000];
        yield 'a fraction' => ['1.5h', 5_400_000_000_000];
        yield 'negative' => ['-2s', -2_000_000_000];
        yield 'positive' => ['+5ms', 5_000_000];
        yield 'microseconds' => ['300us', 300_000];
        yield 'micro sign' => ["2\u{00B5}s", 2_000];
        yield 'greek mu' => ["2\u{03BC}s", 2_000];
        yield 'nanoseconds' => ['7ns', 7];
        yield 'only a fraction' => ['.5s', 500_000_000];
        yield 'a dot without a fraction' => ['5.s', 5_000_000_000];
        yield 'below a nanosecond' => ['0.0000000001s', 0];
        yield 'the largest' => ['9223372036854775807ns', PHP_INT_MAX];
        yield 'the most hours' => ['2562047h', 9_223_369_200_000_000_000];
        yield 'leading zeros' => ['007s', 7_000_000_000];
    }

    #[DataProvider('durations')]
    public function testParsesAsGoDoes(string $duration, int $nanoseconds): void
    {
        self::assertSame($nanoseconds, Duration::parse($duration));
    }

    /**
     * @return iterable<string, array{string, string}>
     */
    public static function invalid(): iterable
    {
        yield 'empty' => ['', 'time: invalid duration ""'];
        yield 'a sign alone' => ['-', 'time: invalid duration "-"'];
        yield 'a unit alone' => ['s', 'time: invalid duration "s"'];
        yield 'words' => ['30 minutes', 'time: unknown unit " minutes" in duration "30 minutes"'];
        yield 'no unit' => ['5', 'time: missing unit in duration "5"'];
        yield 'a padded zero' => ['00', 'time: missing unit in duration "00"'];
        yield 'a decimal zero' => ['0.0', 'time: missing unit in duration "0.0"'];
        yield 'an unknown unit' => ['5x', 'time: unknown unit "x" in duration "5x"'];
        yield 'hex' => ['0x10', 'time: unknown unit "x" in duration "0x10"'];
        yield 'two dots' => ['1.2.3s', 'time: missing unit in duration "1.2.3s"'];
        yield 'past the largest' => ['9223372036854775808ns', 'time: invalid duration "9223372036854775808ns"'];
        yield 'too many hours' => ['2562048h', 'time: invalid duration "2562048h"'];
        yield 'too many by a fraction' => ['2562047.99h', 'time: invalid duration "2562047.99h"'];
        yield 'too many by a sum' => ['2562047h1h', 'time: invalid duration "2562047h1h"'];
        yield 'twenty digits' => ['99999999999999999999ns', 'time: invalid duration "99999999999999999999ns"'];
    }

    #[DataProvider('invalid')]
    public function testRefusesWhatGoRefusesWithItsMessage(string $duration, string $message): void
    {
        $this->expectException(\InvalidArgumentException::class);
        $this->expectExceptionMessage($message);

        Duration::parse($duration);
    }
}
