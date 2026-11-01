<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\Patches;

use Composer\Package\Locker;
use Drupdater\Agent\Failure;
use Drupdater\Agent\Patches\PatchWriter;
use Drupdater\Agent\Tests\FixtureProject;
use Drupdater\Agent\Tests\Scratch;
use PHPUnit\Framework\TestCase;

final class PatchWriterTest extends TestCase
{
    use Scratch;
    use FixtureProject;

    private const PATCHES = '{
        "drupal/token": {
            "Issue #3123456: [Token fix](https://www.drupal.org/i/3123456)": "patches/token/3123456-local.patch",
            "Remote fix, rerolled": "https://www.drupal.org/files/issues/3200000-9.patch"
        },
        "drupal/empty": {}
    }';

    public function testWritesThePatchesAsGivenEmptyObjectsAndOrderIncluded(): void
    {
        $project = $this->fixtureProject();

        $this->writer($project)->write(self::patches(self::PATCHES));

        $patches = $this->manifest($project)->extra->patches;
        self::assertSame(['drupal/token', 'drupal/empty'], array_keys((array) $patches));
        self::assertEquals(new \stdClass(), $patches->{'drupal/empty'}, 'an empty patch list stays an object');
        self::assertEquals(self::patches(self::PATCHES)->{'drupal/token'}, $patches->{'drupal/token'});
    }

    public function testRehashesTheLockAndChangesNothingElseInIt(): void
    {
        $project = $this->fixtureProject();
        $before = (string) file_get_contents($project . '/composer.lock');

        $this->writer($project)->write(self::patches(self::PATCHES));

        $hash = Locker::getContentHash((string) file_get_contents($project . '/composer.json'));
        self::assertNotSame('4b2f0e1c9a7d3e5f6a8b0c1d2e3f4a5b', $hash);
        self::assertSame(str_replace('4b2f0e1c9a7d3e5f6a8b0c1d2e3f4a5b', $hash, $before), file_get_contents($project . '/composer.lock'));
    }

    public function testKeepsANumericDescriptionAKeyOfAnObject(): void
    {
        $project = $this->fixtureProject();

        $this->writer($project)->write(self::patches('{"drupal/token":{"0":"patches/zero.patch"}}'));

        $manifest = $this->manifest($project);
        self::assertEquals((object) ['0' => 'patches/zero.patch'], $manifest->extra->patches->{'drupal/token'});
        self::assertSame('fixture/site', $manifest->name, 'the re-encoded file keeps everything else');
        self::assertEquals((object) ['cweagans/composer-patches' => true, 'drupal/*' => false], $manifest->config->{'allow-plugins'});
    }

    public function testKeepsTheRestOfComposerJsonAsWritten(): void
    {
        $project = $this->fixtureProject();
        $before = (string) file_get_contents($project . '/composer.json');

        $this->writer($project)->write(self::patches(self::PATCHES));

        $head = substr($before, 0, (int) strpos($before, '"patches"'));
        self::assertStringStartsWith($head, (string) file_get_contents($project . '/composer.json'), 'everything up to extra.patches is untouched');
    }

    public function testRefusesAProjectWithoutALockBeforeWriting(): void
    {
        $project = $this->fixtureProject();
        unlink($project . '/composer.lock');

        $this->assertRefusedUnwritten($project, 'the project has no composer.lock');
    }

    public function testRefusesALockWithoutAContentHashBeforeWriting(): void
    {
        $project = $this->fixtureProject();
        $this->writeFiles($project, ['composer.lock' => '{"packages": [], "packages-dev": []}']);

        $this->assertRefusedUnwritten($project, 'composer.lock has no content-hash');
    }

    private function assertRefusedUnwritten(string $project, string $reason): void
    {
        $before = file_get_contents($project . '/composer.json');
        try {
            $this->writer($project)->write(self::patches(self::PATCHES));
            self::fail('the write was not refused');
        }
        catch (Failure $failure) {
            self::assertSame([Failure::FAILED, $reason], [$failure->kind, $failure->reason]);
        }
        self::assertSame($before, file_get_contents($project . '/composer.json'));
    }

    private function writer(string $project): PatchWriter
    {
        return new PatchWriter($project . '/composer.json', $project . '/composer.lock');
    }

    private static function patches(string $json): \stdClass
    {
        return json_decode($json, false, 512, JSON_THROW_ON_ERROR);
    }
}
