<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\Patches;

use Drupdater\Agent\Failure;
use Drupdater\Agent\Patches\DeclaredPatches;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;

final class DeclaredPatchesTest extends TestCase
{
    public function testKeepTheirDeclarationOrderAndRoundTrip(): void
    {
        $json = '{"z/z":{"b":"2.patch","a":"1.patch"},"a/a":{},"m/m":{"0":"zero.patch"}}';

        $patches = self::patches($json);

        self::assertSame(['z/z', 'a/a', 'm/m'], $patches->packages());
        self::assertSame([['description' => 'b', 'path' => '2.patch'], ['description' => 'a', 'path' => '1.patch']], $patches->of('z/z'));
        self::assertSame($json, json_encode($patches->toJson(), JSON_UNESCAPED_SLASHES));
    }

    public function testARepeatedDescriptionKeepsItsFirstPlaceAndItsLastPath(): void
    {
        self::assertSame(
            [['description' => 'one', 'path' => 'last.patch'], ['description' => 'two', 'path' => 'b.patch']],
            self::patches('{"a/a":{"one":"first.patch","two":"b.patch","one":"last.patch"}}')->of('a/a'),
        );
    }

    public function testNullIsNone(): void
    {
        self::assertSame([], DeclaredPatches::fromJson(null)->packages());
        self::assertSame('{}', json_encode(DeclaredPatches::fromJson(null)->toJson()));
    }

    /**
     * @return iterable<string, array{string, string}>
     */
    public static function notPatchObjects(): iterable
    {
        yield 'a list' => ['["a.patch"]', 'extra.patches is not an object'];
        yield 'a string' => ['"patches.json"', 'extra.patches is not an object'];
        yield 'a package whose patches are a list' => ['{"a/a":{},"b/b":["x.patch"]}', 'the patches of b/b are not an object'];
        yield 'a patch that is no path' => ['{"a/a":{"x":1}}', 'a patch of a/a is not a path'];
    }

    #[DataProvider('notPatchObjects')]
    public function testRefuseWhatIsNotAPatchObjectNamingThePackage(string $json, string $reason): void
    {
        $this->expectExceptionObject(new Failure(Failure::FAILED, $reason));
        self::patches($json);
    }

    public function testRenameInPlaceUnlessTheNewDescriptionIsTaken(): void
    {
        $patches = self::patches('{"a/a":{"one":"1.patch","two":"2.patch","three":"3.patch"}}');

        self::assertTrue($patches->rename('a/a', 'two', 'Issue #1'));
        self::assertFalse($patches->rename('a/a', 'one', 'three'), 'JSON keys are unique: one of the two would be lost');
        self::assertFalse($patches->rename('a/a', 'missing', 'four'));
        self::assertSame(['one', 'Issue #1', 'three'], array_column($patches->of('a/a'), 'description'));
    }

    public function testSetPathAndRemoveDropTheEmptiedPackage(): void
    {
        $patches = self::patches('{"a/a":{"one":"1.patch","two":"2.patch"},"b/b":{"x":"x.patch"}}');

        $patches->setPath('a/a', 'two', 'new.patch');
        $patches->remove('a/a', 'one');
        $patches->remove('b/b', 'x');

        self::assertSame('{"a/a":{"two":"new.patch"}}', json_encode($patches->toJson(), JSON_UNESCAPED_SLASHES));
    }

    private static function patches(string $json): DeclaredPatches
    {
        return DeclaredPatches::fromJson(json_decode($json, false, 512, JSON_THROW_ON_ERROR));
    }
}
