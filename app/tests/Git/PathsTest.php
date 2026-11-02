<?php

declare(strict_types=1);

namespace Drupdater\App\Tests\Git;

use Drupdater\App\Git\Paths;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;

final class PathsTest extends TestCase
{
    /**
     * @return iterable<string, array{string, string, bool}>
     */
    public static function cases(): iterable
    {
        yield 'the dir itself' => ['translations', 'translations', true];
        yield 'a file under it' => ['translations', 'translations/de.po', true];
        yield 'deep under it' => ['config/sync', 'config/sync/a/b.yml', true];
        yield 'a prefix sibling' => ['translations', 'translations-backup/de.po', false];
        yield 'another dir' => ['config', 'web/config/x.yml', false];
        yield 'surrounding slashes' => ['/translations/', '/translations/de.po/', true];
        yield 'an empty dir contains all' => ['', 'anything/at/all', true];
        yield 'a slash alone contains all' => ['/', 'x', true];
        yield 'a file is not under its own name as a dir' => ['a.php', 'a.phpx', false];
        yield 'a dir named 0 is not empty' => ['0', '0', true];
        yield 'a dir named 0 contains only its own' => ['0', '1/x', false];
        yield 'dotted names' => ['yv5.g7/g/0-z2', 'yv5.g7/g/0-z2/5-n25nf.', true];
        yield 'a numeric-looking prefix sibling' => ['1/i6/3-', '1/i6/3-6qh6-.z7/0h8b1-8', false];
    }

    #[DataProvider('cases')]
    public function testContains(string $dir, string $file, bool $want): void
    {
        self::assertSame($want, Paths::contains($dir, $file));
    }

    public function testAnyContains(): void
    {
        self::assertTrue(Paths::anyContains(['a', 'b'], 'b/x'));
        self::assertFalse(Paths::anyContains(['a', 'b'], 'c/x'));
        self::assertFalse(Paths::anyContains([], 'c/x'));
    }
}
