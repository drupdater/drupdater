<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\Command;

use Composer\Console\Application;
use Composer\Factory;
use Composer\IO\NullIO;
use Composer\Package\Locker;
use Composer\Util\Filesystem;
use Drupdater\Agent\Command\AgentCommand;
use Drupdater\Agent\Command\AllowPluginsCommand;
use Drupdater\Agent\Command\WritePatchesCommand;
use Drupdater\Agent\Tests\Scratch;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use Symfony\Component\Console\Tester\CommandTester;

/**
 * drupdater:allow-plugins and drupdater:write-patches, against copies of the fixture project.
 */
final class ComposerJsonWritesTest extends TestCase
{
    use Scratch {
        tearDown as scratchTearDown;
    }

    private const FIXTURE_ALLOW_PLUGINS = ['cweagans/composer-patches' => true, 'drupal/*' => false];

    private string|false $composerHome;
    private string $cwd;

    protected function setUp(): void
    {
        $this->composerHome = getenv('COMPOSER_HOME');
        putenv('COMPOSER_HOME=' . $this->scratchDir());
        $this->cwd = (string) getcwd();
    }

    protected function tearDown(): void
    {
        chdir($this->cwd);
        putenv($this->composerHome === false ? 'COMPOSER_HOME' : 'COMPOSER_HOME=' . $this->composerHome);
        $this->scratchTearDown();
    }

    public function testOpenAnswersTheFixtureAndAppendsAWildcard(): void
    {
        $project = $this->fixtureProject();

        [$status, $reply] = $this->ask(new AllowPluginsCommand(), $project, self::fixture('allow-plugins/open-request.json'));

        self::assertSame(0, $status);
        self::assertEquals(self::fixture('allow-plugins/open-response.json'), $reply);
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

        [, $reply] = $this->ask(new AllowPluginsCommand(), $project, self::fixture('allow-plugins/open-request.json'));

        self::assertSame(['original' => $setting, 'opened' => false], $reply['result']);
        self::assertSame($before, file_get_contents($project . '/composer.json'));
    }

    public function testOpenWithoutASettingReportsNull(): void
    {
        $project = $this->fixtureProject(['allow-plugins' => null]);

        [, $reply] = $this->ask(new AllowPluginsCommand(), $project, self::fixture('allow-plugins/open-request.json'));

        self::assertSame(['original' => null, 'opened' => true], $reply['result']);
        self::assertSame(['*' => true], $this->allowPlugins($project));
    }

    public function testCloseAnswersTheFixtureDenyingEachUndecidedPluginAndDroppingTheWildcard(): void
    {
        $project = $this->fixtureProject(['allow-plugins' => self::FIXTURE_ALLOW_PLUGINS + ['*' => true]]);

        [$status, $reply] = $this->ask(new AllowPluginsCommand(), $project, self::fixture('allow-plugins/close-request.json'));

        self::assertSame(0, $status);
        self::assertEquals(self::fixture('allow-plugins/close-response.json'), $reply);
        self::assertSame(
            self::FIXTURE_ALLOW_PLUGINS + ['dealerdirect/phpcodesniffer-composer-installer' => false],
            $this->allowPlugins($project),
        );
    }

    public function testCloseMatchesRulesAsComposerDoesAnchoredCaseInsensitiveAndWithWildcards(): void
    {
        $original = ['CWEAGANS/*' => true, 'dealerdirect/phpcodesniffer-*' => false];
        $project = $this->fixtureProject(['allow-plugins' => $original + ['*' => true]]);

        [, $reply] = $this->ask(new AllowPluginsCommand(), $project, $this->close($original, true));

        self::assertSame([], $reply['result']['denied']);
        self::assertSame($original, $this->allowPlugins($project));
    }

    public function testCloseDoesNotLetARuleMatchPartOfAName(): void
    {
        $original = ['composer-patches' => true, 'dealerdirect/phpcodesniffer-composer-installer' => true];
        $project = $this->fixtureProject(['allow-plugins' => $original + ['*' => true]]);

        [, $reply] = $this->ask(new AllowPluginsCommand(), $project, $this->close($original, true));

        self::assertSame(['cweagans/composer-patches'], $reply['result']['denied']);
    }

    public function testCloseRestoresTheAbsenceOfASettingWhenNothingIsDenied(): void
    {
        $project = $this->fixtureProject(['allow-plugins' => ['*' => true]], withoutPlugins: true);

        [, $reply] = $this->ask(new AllowPluginsCommand(), $project, $this->close(null, true));

        self::assertSame([], $reply['result']['denied']);
        self::assertObjectNotHasProperty('config', $this->manifest($project), 'config held nothing else');
    }

    public function testCloseKeepsAConfigThatHoldsOtherSettings(): void
    {
        $project = $this->fixtureProject(['allow-plugins' => ['*' => true]], withoutPlugins: true);
        $manifest = $this->manifest($project);
        $manifest->config->{'sort-packages'} = true;
        file_put_contents($project . '/composer.json', json_encode($manifest, JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES) . "\n");

        $this->ask(new AllowPluginsCommand(), $project, $this->close(null, true));

        self::assertEquals((object) ['sort-packages' => true], $this->manifest($project)->config);
    }

    public function testCloseWithoutASettingKeepsTheDenials(): void
    {
        $project = $this->fixtureProject(['allow-plugins' => ['*' => true]]);

        [, $reply] = $this->ask(new AllowPluginsCommand(), $project, $this->close(null, true));

        $denied = ['cweagans/composer-patches', 'dealerdirect/phpcodesniffer-composer-installer'];
        self::assertSame($denied, $reply['result']['denied']);
        self::assertSame(array_fill_keys($denied, false), $this->allowPlugins($project));
    }

    public function testCloseKeepsAnEmptySettingAnEmptyObject(): void
    {
        $project = $this->fixtureProject(['allow-plugins' => ['*' => true]], withoutPlugins: true);

        $this->ask(new AllowPluginsCommand(), $project, $this->close(new \stdClass(), true));

        self::assertEquals(new \stdClass(), $this->manifest($project)->config->{'allow-plugins'});
    }

    public function testCloseOfASettingItNeverOpenedWritesNothing(): void
    {
        $project = $this->fixtureProject(['allow-plugins' => true]);
        $before = file_get_contents($project . '/composer.json');

        [, $reply] = $this->ask(new AllowPluginsCommand(), $project, $this->close(true, false));

        self::assertSame([], $reply['result']['denied']);
        self::assertSame($before, file_get_contents($project . '/composer.json'));
    }

    /**
     * @return iterable<string, array{array<string, mixed>, string}>
     */
    public static function invalidAllowPluginsRequests(): iterable
    {
        yield 'no step' => [[], 'step must be "open" or "close"'];
        yield 'an unknown step' => [['step' => 'shut'], 'step must be "open" or "close"'];
        yield 'opened missing' => [['step' => 'close', 'original' => null], 'opened must be a boolean'];
        yield 'original a string' => [['step' => 'close', 'original' => 'all', 'opened' => true], 'original must be an object, a boolean or null'];
        yield 'original a list' => [['step' => 'close', 'original' => ['a/b'], 'opened' => true], 'original must be an object, a boolean or null'];
    }

    /**
     * @param array<string, mixed> $request
     */
    #[DataProvider('invalidAllowPluginsRequests')]
    public function testAllowPluginsRefusesAnInvalidRequestBeforeWriting(array $request, string $reason): void
    {
        $project = $this->fixtureProject();
        $before = file_get_contents($project . '/composer.json');

        [$status, $reply] = $this->ask(new AllowPluginsCommand(), $project, ['protocol' => 2, 'request' => $request]);

        self::assertSame(1, $status);
        self::assertSame(['kind' => 'invalid_request', 'reason' => $reason, 'message' => ''], $reply['error']);
        self::assertSame($before, file_get_contents($project . '/composer.json'));
    }

    public function testWritePatchesAnswersTheFixtureAndWritesThePatchesAsGiven(): void
    {
        $project = $this->fixtureProject();
        $request = self::fixture('write-patches/request.json');

        [$status, $reply] = $this->askVerbatim(new WritePatchesCommand(), $project, 'write-patches/request.json');

        self::assertSame(0, $status);
        self::assertEquals(self::fixture('write-patches/response.json'), $reply);
        $patches = $this->manifest($project)->extra->patches;
        self::assertSame(['drupal/token', 'drupal/empty'], array_keys((array) $patches));
        self::assertEquals(new \stdClass(), $patches->{'drupal/empty'}, 'an empty patch list stays an object');
        self::assertSame($request['request']['patches']['drupal/token'], (array) $patches->{'drupal/token'});
    }

    public function testWritePatchesRehashesTheLockAndChangesNothingElseInIt(): void
    {
        $project = $this->fixtureProject();
        $before = (string) file_get_contents($project . '/composer.lock');

        $this->askVerbatim(new WritePatchesCommand(), $project, 'write-patches/request.json');

        $hash = Locker::getContentHash((string) file_get_contents($project . '/composer.json'));
        $after = (string) file_get_contents($project . '/composer.lock');
        self::assertNotSame('4b2f0e1c9a7d3e5f6a8b0c1d2e3f4a5b', $hash);
        self::assertSame(str_replace('4b2f0e1c9a7d3e5f6a8b0c1d2e3f4a5b', $hash, $before), $after);
    }

    public function testWritePatchesKeepsANumericDescriptionAKeyOfAnObject(): void
    {
        $project = $this->fixtureProject();
        $body = '{"protocol":2,"request":{"patches":{"drupal/token":{"0":"patches/zero.patch"}}}}';

        $tester = $this->tester(new WritePatchesCommand(), $project);
        $tester->setInputs([$body]);
        self::assertSame(0, $tester->execute([]));

        $manifest = $this->manifest($project);
        self::assertEquals((object) ['0' => 'patches/zero.patch'], $manifest->extra->patches->{'drupal/token'});
        self::assertSame('fixture/site', $manifest->name, 'the re-encoded file keeps everything else');
        self::assertEquals((object) self::FIXTURE_ALLOW_PLUGINS, $manifest->config->{'allow-plugins'});
    }

    public function testWritePatchesKeepsTheRestOfComposerJsonAsWritten(): void
    {
        $project = $this->fixtureProject();
        $before = (string) file_get_contents($project . '/composer.json');

        $this->askVerbatim(new WritePatchesCommand(), $project, 'write-patches/request.json');

        $after = (string) file_get_contents($project . '/composer.json');
        $head = substr($before, 0, (int) strpos($before, '"patches"'));
        self::assertStringStartsWith($head, $after, 'everything up to extra.patches is untouched');
    }

    public function testWritePatchesRefusesAProjectWithoutALockBeforeWriting(): void
    {
        $project = $this->fixtureProject();
        unlink($project . '/composer.lock');
        $before = file_get_contents($project . '/composer.json');

        [$status, $reply] = $this->ask(new WritePatchesCommand(), $project, self::fixture('write-patches/request.json'));

        self::assertSame(1, $status);
        self::assertSame(['failed', 'the project has no composer.lock'], [$reply['error']['kind'], $reply['error']['reason']]);
        self::assertSame($before, file_get_contents($project . '/composer.json'));
    }

    public function testWritePatchesRefusesALockWithoutAContentHashBeforeWriting(): void
    {
        $project = $this->fixtureProject();
        $this->writeFiles($project, ['composer.lock' => '{"packages": [], "packages-dev": []}']);
        $before = file_get_contents($project . '/composer.json');

        [$status, $reply] = $this->ask(new WritePatchesCommand(), $project, self::fixture('write-patches/request.json'));

        self::assertSame(1, $status);
        self::assertSame(['failed', 'composer.lock has no content-hash'], [$reply['error']['kind'], $reply['error']['reason']]);
        self::assertSame($before, file_get_contents($project . '/composer.json'));
    }

    /**
     * @return iterable<string, array{string}>
     */
    public static function patchesThatAreNotObjects(): iterable
    {
        yield 'missing' => ['{"protocol":2,"request":{}}'];
        yield 'a list' => ['{"protocol":2,"request":{"patches":["a.patch"]}}'];
        yield 'a string' => ['{"protocol":2,"request":{"patches":"a.patch"}}'];
    }

    #[DataProvider('patchesThatAreNotObjects')]
    public function testWritePatchesRefusesPatchesThatAreNotAnObject(string $body): void
    {
        $tester = $this->tester(new WritePatchesCommand(), $this->fixtureProject());
        $tester->setInputs([$body]);

        self::assertSame(1, $tester->execute([]));
        $reply = json_decode($tester->getDisplay(), true);
        self::assertSame(['invalid_request', 'patches must be an object'], [$reply['error']['kind'], $reply['error']['reason']]);
    }

    /**
     * @return array<string, mixed>
     */
    private function close(mixed $original, bool $opened): array
    {
        return ['protocol' => 2, 'request' => ['step' => 'close', 'original' => $original, 'opened' => $opened]];
    }

    /**
     * The fixture project, its config.allow-plugins replaced when given (null removes it).
     *
     * @param array{allow-plugins?: mixed} $config
     */
    private function fixtureProject(array $config = [], bool $withoutPlugins = false): string
    {
        $project = $this->scratchDir();
        (new Filesystem())->copy(__DIR__ . '/../fixtures/project', $project);
        if (array_key_exists('allow-plugins', $config)) {
            $manifest = json_decode((string) file_get_contents($project . '/composer.json'), true);
            unset($manifest['config']['allow-plugins']);
            if ($config['allow-plugins'] !== null) {
                $manifest['config']['allow-plugins'] = $config['allow-plugins'];
            }
            $manifest['config'] = (object) $manifest['config'];
            file_put_contents($project . '/composer.json', json_encode($manifest, JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES) . "\n");
        }
        if ($withoutPlugins) {
            $lock = json_decode((string) file_get_contents($project . '/composer.lock'), true);
            foreach (['packages', 'packages-dev'] as $key) {
                $lock[$key] = array_values(array_filter(
                    $lock[$key],
                    static fn (array $package): bool => !in_array($package['type'], ['composer-plugin', 'composer-installer'], true),
                ));
            }
            file_put_contents($project . '/composer.lock', json_encode($lock, JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES));
        }
        return $project;
    }

    private function manifest(string $project): \stdClass
    {
        return json_decode((string) file_get_contents($project . '/composer.json'), false, 512, JSON_THROW_ON_ERROR);
    }

    /**
     * @return array<string, bool>
     */
    private function allowPlugins(string $project): array
    {
        return (array) $this->manifest($project)->config->{'allow-plugins'};
    }

    /**
     * @param array<string, mixed> $request
     * @return array{int, array<string, mixed>}
     */
    private function ask(AgentCommand $command, string $project, array $request): array
    {
        $tester = $this->tester($command, $project);
        $tester->setInputs([json_encode($request, JSON_THROW_ON_ERROR)]);
        $status = $tester->execute([]);
        return [$status, json_decode($tester->getDisplay(), true, 512, JSON_THROW_ON_ERROR)];
    }

    /**
     * Sends a protocol fixture's bytes, so its {} reach the command as {}.
     *
     * @return array{int, array<string, mixed>}
     */
    private function askVerbatim(AgentCommand $command, string $project, string $fixture): array
    {
        $tester = $this->tester($command, $project);
        $tester->setInputs([(string) file_get_contents(__DIR__ . '/../../protocol/' . $fixture)]);
        $status = $tester->execute([]);
        return [$status, json_decode($tester->getDisplay(), true, 512, JSON_THROW_ON_ERROR)];
    }

    private function tester(AgentCommand $command, string $project): CommandTester
    {
        chdir($project);
        $command->setComposer((new Factory())->createComposer(new NullIO(), $project . '/composer.json', true, $project));
        $application = new Application();
        $application->setAutoExit(false);
        $application->add($command);
        return new CommandTester($command);
    }
}
