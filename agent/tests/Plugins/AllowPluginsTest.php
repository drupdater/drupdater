<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\Plugins;

use Drupdater\Agent\Plugins\AllowPlugins;
use Drupdater\Agent\Tests\FixtureProject;
use Drupdater\Agent\Tests\InProject;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;

final class AllowPluginsTest extends TestCase
{
    use InProject;
    use FixtureProject;

    private const FIXTURE_ALLOW_PLUGINS = ['cweagans/composer-patches' => true, 'drupal/*' => false];

    public function testOpenRecordsTheSettingAndAppendsAWildcard(): void
    {
        $project = $this->fixtureProject();

        $opened = $this->plugins($project)->open();

        self::assertEquals(['original' => (object) self::FIXTURE_ALLOW_PLUGINS, 'opened' => true], $opened);
        self::assertSame(self::FIXTURE_ALLOW_PLUGINS + ['*' => true], $this->allowPlugins($project));
    }

    /**
     * @return iterable<string, array{mixed}>
     */
    public static function settingsThatDecideEverything(): iterable
    {
        yield 'true' => [true];
        yield 'false' => [false];
        yield 'a wildcard rule' => [['drupal/*' => false, '*' => true]];
    }

    #[DataProvider('settingsThatDecideEverything')]
    public function testOpenLeavesASettingThatDecidesEveryPluginAlone(mixed $setting): void
    {
        $project = $this->fixtureProject(['allow-plugins' => $setting]);
        $before = file_get_contents($project . '/composer.json');

        $opened = $this->plugins($project)->open();

        self::assertEquals(['original' => is_array($setting) ? (object) $setting : $setting, 'opened' => false], $opened);
        self::assertSame($before, file_get_contents($project . '/composer.json'));
    }

    public function testOpenWithoutASettingRecordsNull(): void
    {
        $project = $this->fixtureProject(['allow-plugins' => null]);

        self::assertSame(['original' => null, 'opened' => true], $this->plugins($project)->open());
        self::assertSame(['*' => true], $this->allowPlugins($project));
    }

    public function testCloseDeniesEachUndecidedPluginAndDropsTheWildcard(): void
    {
        $project = $this->fixtureProject(['allow-plugins' => self::FIXTURE_ALLOW_PLUGINS + ['*' => true]]);

        $denied = $this->plugins($project)->close((object) self::FIXTURE_ALLOW_PLUGINS, true);

        self::assertSame(['dealerdirect/phpcodesniffer-composer-installer'], $denied);
        self::assertSame(self::FIXTURE_ALLOW_PLUGINS + ['dealerdirect/phpcodesniffer-composer-installer' => false], $this->allowPlugins($project));
    }

    public function testCloseMatchesRulesAsComposerDoesAnchoredCaseInsensitiveAndWithWildcards(): void
    {
        $original = ['CWEAGANS/*' => true, 'dealerdirect/phpcodesniffer-*' => false];
        $project = $this->fixtureProject(['allow-plugins' => $original + ['*' => true]]);

        self::assertSame([], $this->plugins($project)->close((object) $original, true));
        self::assertSame($original, $this->allowPlugins($project));
    }

    public function testCloseDoesNotLetARuleMatchPartOfAName(): void
    {
        $original = ['composer-patches' => true, 'dealerdirect/phpcodesniffer-composer-installer' => true];
        $project = $this->fixtureProject(['allow-plugins' => $original + ['*' => true]]);

        self::assertSame(['cweagans/composer-patches'], $this->plugins($project)->close((object) $original, true));
    }

    public function testCloseRestoresTheAbsenceOfASettingWhenNothingIsDenied(): void
    {
        $project = $this->fixtureProject(['allow-plugins' => ['*' => true]], withoutPlugins: true);

        self::assertSame([], $this->plugins($project)->close(null, true));
        self::assertObjectNotHasProperty('config', $this->manifest($project), 'config held nothing else');
    }

    public function testCloseKeepsAConfigThatHoldsOtherSettings(): void
    {
        $project = $this->fixtureProject(['allow-plugins' => ['*' => true]], withoutPlugins: true);
        $manifest = $this->manifest($project);
        $manifest->config->{'sort-packages'} = true;
        file_put_contents($project . '/composer.json', json_encode($manifest, JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES) . "\n");

        $this->plugins($project)->close(null, true);

        self::assertEquals((object) ['sort-packages' => true], $this->manifest($project)->config);
    }

    public function testCloseWithoutASettingKeepsTheDenials(): void
    {
        $project = $this->fixtureProject(['allow-plugins' => ['*' => true]]);

        $denied = $this->plugins($project)->close(null, true);

        $expected = ['cweagans/composer-patches', 'dealerdirect/phpcodesniffer-composer-installer'];
        self::assertSame($expected, $denied);
        self::assertSame(array_fill_keys($expected, false), $this->allowPlugins($project));
    }

    public function testCloseKeepsAnEmptySettingAnEmptyObject(): void
    {
        $project = $this->fixtureProject(['allow-plugins' => ['*' => true]], withoutPlugins: true);

        $this->plugins($project)->close(new \stdClass(), true);

        self::assertEquals(new \stdClass(), $this->manifest($project)->config->{'allow-plugins'});
    }

    public function testCloseOfASettingItNeverOpenedWritesNothing(): void
    {
        $project = $this->fixtureProject(['allow-plugins' => true]);
        $before = file_get_contents($project . '/composer.json');

        self::assertSame([], $this->plugins($project)->close(true, false));
        self::assertSame($before, file_get_contents($project . '/composer.json'));
    }

    private function plugins(string $project): AllowPlugins
    {
        return new AllowPlugins($this->composer($project), $project . '/composer.json');
    }

    /**
     * @return array<string, bool>
     */
    private function allowPlugins(string $project): array
    {
        return (array) $this->manifest($project)->config->{'allow-plugins'};
    }
}
