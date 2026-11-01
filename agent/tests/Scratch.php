<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests;

use Composer\Util\Filesystem;

trait Scratch
{
    /** @var list<string> */
    private array $scratchDirs = [];

    protected function scratchDir(): string
    {
        $dir = sys_get_temp_dir() . '/drupdater-agent-test-' . bin2hex(random_bytes(6));
        mkdir($dir, 0777, true);
        $this->scratchDirs[] = $dir;
        return $dir;
    }

    /**
     * @param array<string, string> $files
     */
    protected function writeFiles(string $dir, array $files): void
    {
        foreach ($files as $path => $content) {
            (new Filesystem())->ensureDirectoryExists(dirname($dir . '/' . $path));
            file_put_contents($dir . '/' . $path, $content);
        }
    }

    protected function tearDown(): void
    {
        foreach ($this->scratchDirs as $dir) {
            (new Filesystem())->removeDirectory($dir);
        }
        parent::tearDown();
    }

    /**
     * @return array<string, mixed>
     */
    protected static function fixture(string $name): array
    {
        return json_decode((string) file_get_contents(__DIR__ . '/../protocol/' . $name), true, 512, JSON_THROW_ON_ERROR);
    }

    protected const GREETING_PATCH = <<<'PATCH'
        --- a/src/greeting.txt
        +++ b/src/greeting.txt
        @@ -1 +1 @@
        -hello
        +hello world

        PATCH;

    protected const FAREWELL_PATCH = <<<'PATCH'
        --- a/src/farewell.txt
        +++ b/src/farewell.txt
        @@ -1 +1 @@
        -goodbye
        +goodbye world

        PATCH;

    protected const PACKAGE_FILES = ['src/greeting.txt' => "hello\n", 'src/farewell.txt' => "goodbye\n"];
}
