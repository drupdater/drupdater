<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\Command;

use Composer\Console\Application;
use Drupdater\Agent\Command\AgentCommand;
use Drupdater\Agent\Command\ConfigExportCommand;
use Drupdater\Agent\Command\SiteDatabaseCommand;
use Drupdater\Agent\Command\SiteFactsCommand;
use Drupdater\Agent\Command\SiteInstallCommand;
use Drupdater\Agent\Command\SiteRestoreCommand;
use Drupdater\Agent\Command\SiteUpdateCommand;
use Drupdater\Agent\Site\TestDatabase;
use Drupdater\Agent\Tests\InProject;
use Drupdater\Agent\Tools\Drush;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use Symfony\Component\Console\Tester\CommandTester;

/**
 * The per-site commands, against a project whose drush is a script that answers as drush would
 * and logs what it was asked, one line per run: SITE_NAME, then the arguments.
 */
final class SiteCommandsTest extends TestCase
{
    use InProject;

    private const DRUSH = <<<'SH'
        #!/bin/sh
        PWD=$(pwd)
        echo "$SITE_NAME $*" >> "$PWD/drush.log"
        case "$*" in
          *drupdater:facts*--skip-update-status*) cat "$PWD/facts-after.json" ;;
          *drupdater:facts*) cat "$PWD/facts.json" ;;
          *status*) printf '{"config-sync":"../config/sync","root":"%s/web"}' "$PWD" ;;
          *updatedb*) [ -f "$PWD/fail" ] && { echo "SQLSTATE: no such table" >&2; exit 1; } ;;
        esac
        exit 0
        SH;

    private string $project;

    public function testSiteFactsAnswersTheFixture(): void
    {
        $this->project();

        [$status, $reply, $raw] = $this->ask(new SiteFactsCommand(), self::fixture('site-facts/request.json'));

        self::assertSame(0, $status);
        self::assertEquals(self::fixture('site-facts/response.json'), self::withoutLog($reply));
        self::assertSame(['default --include=' . Drush::include() . ' drupdater:facts'], $this->drushLog());
        self::assertStringContainsString('"unsupported_modules_error":""', $raw);
    }

    public function testSiteFactsWithoutUpdateStatusSkipsDrupalOrg(): void
    {
        $this->project();
        $request = self::fixture('site-facts/request.json');
        $request['request']['update_status'] = false;

        $this->ask(new SiteFactsCommand(), $request);

        self::assertSame(['default --include=' . Drush::include() . ' drupdater:facts --skip-update-status'], $this->drushLog());
    }

    public function testSiteFactsNeedsToKnowWhetherToAskDrupalOrg(): void
    {
        $this->project();

        [$status, $reply] = $this->ask(new SiteFactsCommand(), ['protocol' => 5, 'request' => ['site' => 'default']]);

        self::assertSame(1, $status);
        self::assertSame(['kind' => 'invalid_request', 'reason' => 'update_status must be a boolean', 'message' => ''], $reply['error']);
    }

    public function testSiteUpdateRunsTheHooksResavesAndAnswersTheFixtureWithObjectsKept(): void
    {
        $this->project();

        [$status, $reply, $raw] = $this->ask(new SiteUpdateCommand(), self::fixture('site-update/request.json'));

        self::assertSame(0, $status);
        self::assertEquals(self::fixture('site-update/response.json'), self::withoutLog($reply));
        self::assertStringContainsString('"update_hooks":{}', $raw);
        self::assertSame([
            'default updatedb --yes',
            'default --include=' . Drush::include() . ' drupdater:config-resave',
            'default --include=' . Drush::include() . ' drupdater:facts --skip-update-status',
        ], $this->drushLog());
    }

    public function testAFailingUpdateHookFailsTheSiteUpdateWithDrushsOutput(): void
    {
        $this->project();
        touch($this->project . '/fail');

        [$status, $reply] = $this->ask(new SiteUpdateCommand(), self::fixture('site-update/request.json'));

        self::assertSame(1, $status);
        self::assertSame(['failed', 'drush updatedb --yes failed'], [$reply['error']['kind'], $reply['error']['reason']]);
        self::assertStringContainsString('SQLSTATE: no such table', $reply['error']['message']);
        self::assertSame(['default updatedb --yes'], $this->drushLog(), 'nothing runs after a failed update');
    }

    public function testConfigExportAnswersTheFixtureWithThePathRelativeToTheProject(): void
    {
        $this->project();

        [$status, $reply] = $this->ask(new ConfigExportCommand(), self::fixture('config-export/request.json'));

        self::assertSame(0, $status);
        self::assertEquals(self::fixture('config-export/response.json'), self::withoutLog($reply));
        self::assertSame(['default config:export --yes', 'default status --fields=config-sync,root --format=json'], $this->drushLog());
    }

    public function testSiteDatabaseAndSiteRestoreAnswerTheirFixtures(): void
    {
        $this->project();
        $data = $this->scratchDir();

        [$configured, $configureReply] = $this->ask(new SiteDatabaseCommand(), $this->withDataDir('site-database/request.json', $data));
        $settings = (string) file_get_contents($this->project . '/web/sites/default/settings.php');
        [$restored, $restoreReply] = $this->ask(new SiteRestoreCommand(), $this->withDataDir('site-restore/request.json', $data));

        self::assertSame([0, 0], [$configured, $restored]);
        self::assertEquals(self::fixture('site-database/response.json'), self::withoutLog($configureReply));
        self::assertEquals(self::fixture('site-restore/response.json'), self::withoutLog($restoreReply));
        self::assertStringContainsString(TestDatabase::MARKER, $settings);
        self::assertStringContainsString("'database' => '" . $data . "/default.sqlite'", $settings);
        self::assertSame("<?php\n", file_get_contents($this->project . '/web/sites/default/settings.php'));
    }

    public function testSiteInstallAnswersTheFixtureAndPutsSettingsBack(): void
    {
        $this->project();

        [$status, $reply] = $this->ask(new SiteInstallCommand(), $this->withDataDir('site-install/request.json', $this->scratchDir()));

        self::assertSame(0, $status);
        self::assertEquals(self::fixture('site-install/response.json'), self::withoutLog($reply));
        self::assertSame('default --existing-config --yes site:install --sites-subdir=default --verbose', $this->drushLog()[1]);
        self::assertSame("<?php\n", file_get_contents($this->project . '/web/sites/default/settings.php'));
        self::assertSame('installing site', $reply['log'][0]['message'] ?? null, 'what it logged reaches drupdater');
    }

    /**
     * @return iterable<string, array{mixed}>
     */
    public static function badDataDirs(): iterable
    {
        yield 'missing' => [null];
        yield 'relative' => ['data'];
        yield 'not a directory' => ['/nonexistent/drupdater-data'];
    }

    #[DataProvider('badDataDirs')]
    public function testTheDatabaseCommandsNeedAnExistingDataDirectory(mixed $dataDir): void
    {
        $this->project();
        foreach ([new SiteInstallCommand(), new SiteDatabaseCommand(), new SiteRestoreCommand()] as $command) {
            [$status, $reply] = $this->ask($command, ['protocol' => 5, 'request' => ['site' => 'default', 'data_dir' => $dataDir]]);

            self::assertSame(1, $status);
            self::assertSame(['kind' => 'invalid_request', 'reason' => 'data_dir must be an existing absolute directory', 'message' => ''], $reply['error']);
        }
        self::assertSame([], $this->drushLog());
    }

    /**
     * @return iterable<string, array{mixed}>
     */
    public static function badSites(): iterable
    {
        yield 'missing' => [null];
        yield 'empty' => [''];
        yield 'not a string' => [1];
        yield 'the parent' => ['..'];
        yield 'itself' => ['.'];
        yield 'a path' => ['../default'];
    }

    #[DataProvider('badSites')]
    public function testEverySiteCommandNeedsASiteDirectoryName(mixed $site): void
    {
        $this->project();
        $commands = [new SiteFactsCommand(), new SiteUpdateCommand(), new ConfigExportCommand(), new SiteInstallCommand(), new SiteDatabaseCommand(), new SiteRestoreCommand()];
        foreach ($commands as $command) {
            [$status, $reply] = $this->ask($command, ['protocol' => 5, 'request' => ['site' => $site, 'update_status' => true, 'data_dir' => $this->project]]);

            self::assertSame(1, $status, (string) $command->getName());
            self::assertSame(['kind' => 'invalid_request', 'reason' => 'site must name a directory under sites/', 'message' => ''], $reply['error']);
        }
        self::assertSame([], $this->drushLog());
    }

    private function project(): void
    {
        $this->project = $this->scratchDir();
        $facts = self::rawFixture('site-facts/response.json')->result->facts;
        $after = self::rawFixture('site-update/response.json')->result->facts;
        $this->writeFiles($this->project, [
            'composer.json' => json_encode([
                'name' => 'fixture/site',
                'repositories' => [['packagist.org' => false]],
                'extra' => ['drupal-scaffold' => ['locations' => ['web-root' => 'web/']]],
            ], JSON_UNESCAPED_SLASHES),
            'vendor/bin/drush' => self::DRUSH,
            'facts.json' => (string) json_encode($facts, JSON_UNESCAPED_SLASHES),
            'facts-after.json' => (string) json_encode($after, JSON_UNESCAPED_SLASHES),
            'web/sites/default/settings.php' => "<?php\n",
            'config/sync/core.extension.yml' => "module:\n  node: 0\n  sqlite: 0\ntheme:\n  stark: 0\nprofile: minimal\n",
        ]);
        chmod($this->project . '/vendor/bin/drush', 0755);
    }

    /**
     * @return list<string>
     */
    private function drushLog(): array
    {
        $log = @file_get_contents($this->project . '/drush.log');
        return $log === false ? [] : explode("\n", rtrim($log, "\n"));
    }

    /**
     * @return array<string, mixed>
     */
    private function withDataDir(string $fixture, string $dataDir): array
    {
        $request = self::fixture($fixture);
        $request['request']['data_dir'] = $dataDir;
        return $request;
    }

    /**
     * @param array<string, mixed> $reply
     * @return array<string, mixed>
     */
    private static function withoutLog(array $reply): array
    {
        unset($reply['log']);
        return $reply;
    }

    private static function rawFixture(string $name): \stdClass
    {
        return json_decode((string) file_get_contents(__DIR__ . '/../../protocol/' . $name), false, 512, JSON_THROW_ON_ERROR);
    }

    /**
     * @param array<string, mixed> $request
     * @return array{int, array<string, mixed>, string}
     */
    private function ask(AgentCommand $command, array $request): array
    {
        $command->setComposer($this->composer($this->project));
        $application = new Application();
        $application->setAutoExit(false);
        $application->add($command);
        $tester = new CommandTester($command);
        $tester->setInputs([json_encode($request, JSON_THROW_ON_ERROR)]);
        $status = $tester->execute([]);
        $raw = trim($tester->getDisplay());
        return [$status, json_decode($raw, true, 512, JSON_THROW_ON_ERROR), $raw];
    }
}
