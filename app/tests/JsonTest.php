<?php

declare(strict_types=1);

namespace Drupdater\App\Tests;

use Drupdater\App\Json;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;

final class JsonTest extends TestCase
{
    public function testAtWalksArraysAndObjectsAlike(): void
    {
        $decoded = json_decode('{"a":[{"b":"x"}]}', false, 512, JSON_THROW_ON_ERROR);

        self::assertSame('x', Json::at($decoded, 'a', 0, 'b'));
        self::assertSame('x', Json::at(['a' => [(object) ['b' => 'x']]], 'a', 0, 'b'));
        self::assertSame($decoded, Json::at($decoded), 'no path is the value itself');
    }

    public function testAtIsNullWhereThePathRunsOut(): void
    {
        self::assertNull(Json::at(['a' => 1], 'b'));
        self::assertNull(Json::at(['a' => 1], 'a', 'b'), 'a scalar has no members');
        self::assertNull(Json::at((object) ['a' => null], 'a', 'b'));
        self::assertNull(Json::at(null, 0));
    }

    public function testMapKeysEveryMemberByString(): void
    {
        self::assertSame(['0' => 'x', 'k' => 'y'], Json::map(['x', 'k' => 'y']));
        self::assertSame(['k' => ['v']], Json::map((object) ['k' => ['v']]));
    }

    public function testListKeepsTheValuesInOrder(): void
    {
        self::assertSame(['y', 'x'], Json::list(['b' => 'y', 'a' => 'x']));
        self::assertSame([1, 2], Json::list((object) ['a' => 1, 'b' => 2]));
    }

    public function testMapsSkipsWhatIsNotAnObject(): void
    {
        self::assertSame([['a' => 1], ['b' => 2]], Json::maps([['a' => 1], 'x', 3, null, (object) ['b' => 2]]));
    }

    public function testStringReadsAScalarAndDefaultsTheRest(): void
    {
        self::assertSame('x', Json::string('x'));
        self::assertSame('3', Json::string(3));
        self::assertSame('1.5', Json::string(1.5));
        self::assertSame('1', Json::string(true));
        self::assertSame('', Json::string(null));
        self::assertSame('-', Json::string(['x'], '-'));
    }

    public function testIntReadsANumber(): void
    {
        self::assertSame(3, Json::int(3));
        self::assertSame(3, Json::int('3'));
        self::assertSame(2, Json::int(2.9));
        self::assertSame(0, Json::int('three'));
        self::assertSame(0, Json::int(true), 'a boolean is not a count');
    }

    public function testStringsSkipsWhatIsNotAScalar(): void
    {
        self::assertSame(['a', '2'], Json::strings(['a', ['b'], 2, null]));
    }

    /**
     * @return iterable<string, array{mixed}>
     */
    public static function notContainers(): iterable
    {
        yield 'null' => [null];
        yield 'string' => ['abc'];
        yield 'number' => [3];
        yield 'boolean' => [true];
    }

    #[DataProvider('notContainers')]
    public function testWhatIsNotAContainerReadsAsEmpty(mixed $value): void
    {
        self::assertSame([], Json::map($value));
        self::assertSame([], Json::list($value));
        self::assertSame([], Json::maps($value));
        self::assertSame([], Json::strings($value));
    }
}
