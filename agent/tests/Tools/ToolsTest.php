<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\Tools;

use Drupdater\Agent\Failure;
use Drupdater\Agent\Process\ProcessResult;
use Drupdater\Agent\Process\ProcessRunner;
use Drupdater\Agent\Tests\FakeProcessRunner;
use Drupdater\Agent\Tests\Scratch;
use Drupdater\Agent\Tools\Bin;
use Drupdater\Agent\Tools\ComposerCli;
use Drupdater\Agent\Tools\Drush;
use Drupdater\Agent\Tools\Phpcs;
use Drupdater\Agent\Tools\Rector;
use PHPUnit\Framework\TestCase;

/**
 * phpcs, Rector, drush and Composer as the addons run them: which command, where, and what they
 * make of its answer.
 */
final class ToolsTest extends TestCase
{
    use Scratch;

    public function testABinRunsItsToolFromItselfWithItselfFirstOnPath(): void
    {
        $bin = new Bin('/project/vendor/bin');

        self::assertSame(['/project/vendor/bin/drush'], $bin->command('drush'));
        self::assertStringStartsWith('/project/vendor/bin' . PATH_SEPARATOR, $bin->env()['PATH']);
    }

    public function testPhpcsReportsTheViolationsOfWhatItsConfigCovers(): void
    {
        $runner = new FakeProcessRunner([new ProcessResult(0, '{"files":{"/p/a.php":{"errors":1,"warnings":2,"messages":[]}},"totals":{"errors":1,"warnings":2,"fixable":3}}', 'PHP Notice: noise')]);

        $report = (new Phpcs($runner, new Bin('/bin'), '/p'))->report();

        self::assertSame(['files' => ['/p/a.php' => ['errors' => 1, 'warnings' => 2]], 'totals' => ['errors' => 1, 'warnings' => 2, 'fixable' => 3]], $report);
        self::assertSame(['/bin/phpcs', '--report=json', '-q', '--runtime-set', 'ignore_errors_on_exit', '1', '--runtime-set', 'ignore_warnings_on_exit', '1'], $runner->runs[0]['command']);
        self::assertSame('/p', $runner->runs[0]['cwd']);
    }

    public function testPhpcsThatFailsOrPrintsNoReportIsAFailure(): void
    {
        foreach ([new ProcessResult(3, '', 'boom'), new ProcessResult(0, 'not json', '')] as $result) {
            try {
                (new Phpcs(new FakeProcessRunner([$result]), new Bin('/bin'), '/p'))->report();
                self::fail('accepted a failed phpcs');
            }
            catch (Failure $failure) {
                self::assertSame(Failure::FAILED, $failure->kind);
            }
        }
    }

    public function testPhpcbfThatLeavesViolationsIsAFailureTheAddonCanLog(): void
    {
        $runner = new FakeProcessRunner([new ProcessResult(0, '', ''), new ProcessResult(1, 'remaining', '')]);
        $phpcs = new Phpcs($runner, new Bin('/bin'), '/p');

        $phpcs->fix();
        self::assertFailure('phpcbf failed', $phpcs->fix(...));
    }

    public function testRectorRewritesTheDirectoriesAndRestoresAnyFileDocBlockItMoved(): void
    {
        $project = $this->scratchDir();
        $this->writeFiles($project, ['web/modules/custom/demo.module' => "<?php\n\nuse A\\B;\n\n/**\n * @file\n * Demo.\n */\n\nfunction demo() {}\n"]);
        $runner = new FakeProcessRunner([new ProcessResult(0, json_encode([
            'totals' => ['changed_files' => 1, 'errors' => 0],
            'file_diffs' => [['file' => 'web/modules/custom/demo.module', 'diff' => '...', 'applied_rectors' => ['R']]],
            'changed_files' => ['web/modules/custom/demo.module'],
        ]), '')]);

        $result = Rector::project($runner, new Bin('/bin'), '/agent/rector.php', $project)->process(['web/modules/custom']);

        self::assertSame(['changed_files' => ['web/modules/custom/demo.module'], 'file_diffs' => [['file' => 'web/modules/custom/demo.module', 'applied_rectors' => ['R']]]], $result);
        self::assertSame(['/bin/rector', 'process', '--config=/agent/rector.php', '--no-progress-bar', '--debug', '--output-format=json', 'web/modules/custom'], $runner->runs[0]['command']);
        self::assertStringStartsWith("<?php\n\n/**\n * @file", (string) file_get_contents($project . '/web/modules/custom/demo.module'));
    }

    public function testOurRectorLoadsTheProjectsAutoloaderWhereverVendorDirPutsIt(): void
    {
        $runner = new FakeProcessRunner([new ProcessResult(0, '{"file_diffs":[],"changed_files":[]}', '')]);

        Rector::bundled($runner, new Bin('/tools'), '/agent/rector.php', '/p', '/p/lib')->process(['web/modules/custom']);

        self::assertSame(['/tools/rector', 'process', '--autoload-file', '/p/lib/autoload.php', '--config=/agent/rector.php'], array_slice($runner->runs[0]['command'], 0, 5));
    }

    public function testRectorWithNoCustomCodeRunsNothing(): void
    {
        self::assertSame(['changed_files' => [], 'file_diffs' => []], Rector::project(new FakeProcessRunner(), new Bin('/bin'), 'c', '/p')->process([]));
    }

    public function testRectorThatFailsOrChangedAFileThatIsGoneIsAFailure(): void
    {
        $gone = json_encode(['file_diffs' => [['file' => 'missing.php', 'applied_rectors' => []]], 'changed_files' => ['missing.php']]);
        foreach ([new ProcessResult(1, '', 'boom'), new ProcessResult(0, '{', ''), new ProcessResult(0, (string) $gone, '')] as $result) {
            try {
                Rector::project(new FakeProcessRunner([$result]), new Bin('/bin'), 'c', $this->scratchDir())->process(['web']);
                self::fail('accepted a failed rector');
            }
            catch (Failure $failure) {
                self::assertSame(Failure::FAILED, $failure->kind);
            }
        }
    }

    public function testDrushRunsAgainstTheSiteItIsGiven(): void
    {
        $runner = new FakeProcessRunner([new ProcessResult(0, '', '')]);

        (new Drush($runner, new Bin('/p/vendor/bin'), '/p'))->localizeTranslations('example.com');

        self::assertSame(['/p/vendor/bin/drush', 'locale-deploy:localize-translations'], $runner->runs[0]['command']);
        self::assertSame('example.com', $runner->runs[0]['env']['SITE_NAME']);
    }

    public function testAFailedDrushIsAFailureNamingTheCommand(): void
    {
        self::assertFailure('drush locale-deploy:localize-translations failed', static fn () => (new Drush(new FakeProcessRunner([new ProcessResult(1, '', 'no db')]), new Bin('/bin'), '/p'))->localizeTranslations('default'));
    }

    public function testDrushReadsTheFactsThroughDrupdatersOwnCommandKeepingObjects(): void
    {
        $runner = new FakeProcessRunner([new ProcessResult(0, '{"modules":["node"],"update_hooks":{}}', 'PHP Notice: noise'), new ProcessResult(0, '{}', '')]);
        $drush = new Drush($runner, new Bin('/p/vendor/bin'), '/p', '/agent/resources/drush');

        $facts = $drush->facts('example.com', true);
        $drush->facts('example.com', false);

        self::assertSame('{"modules":["node"],"update_hooks":{}}', json_encode($facts));
        self::assertSame(['/p/vendor/bin/drush', '--include=/agent/resources/drush', 'drupdater:facts'], $runner->runs[0]['command']);
        self::assertSame(['/p/vendor/bin/drush', '--include=/agent/resources/drush', 'drupdater:facts', '--skip-update-status'], $runner->runs[1]['command']);
        self::assertSame(['/p', 'example.com'], [$runner->runs[0]['cwd'], $runner->runs[0]['env']['SITE_NAME']]);
    }

    public function testDrushFactsThatAreNoObjectAreAFailure(): void
    {
        self::assertFailure('drush drupdater:facts printed no JSON object', static fn () => (new Drush(new FakeProcessRunner([new ProcessResult(0, '[]', 'x')]), new Bin('/bin'), '/p'))->facts('default', true));
        self::assertFailure('drush drupdater:facts printed no JSON report', static fn () => (new Drush(new FakeProcessRunner([new ProcessResult(0, 'Notice', '')]), new Bin('/bin'), '/p'))->facts('default', true));
    }

    public function testAFailedDrushIsNamedWithoutTheIncludePath(): void
    {
        self::assertFailure('drush drupdater:config-resave failed', static fn () => (new Drush(new FakeProcessRunner([new ProcessResult(1, '', 'no db')]), new Bin('/bin'), '/p', '/agent/resources/drush'))->resaveConfig('default'));
    }

    public function testDrushRunsTheSitesUpdateExportAndInstall(): void
    {
        $runner = new FakeProcessRunner(array_fill(0, 4, new ProcessResult(0, '', '')));
        $drush = new Drush($runner, new Bin('/bin'), '/p', '/inc');

        $drush->updateDatabase('a');
        $drush->resaveConfig('a');
        $drush->exportConfig('a');
        $drush->installSite('a');

        self::assertSame([
            ['/bin/drush', 'updatedb', '--yes'],
            ['/bin/drush', '--include=/inc', 'drupdater:config-resave'],
            ['/bin/drush', 'config:export', '--yes'],
            ['/bin/drush', '--existing-config', '--yes', 'site:install', '--sites-subdir=a', '--verbose'],
        ], array_column($runner->runs, 'command'));
    }

    /**
     * @return iterable<string, array{string, string}>
     */
    public static function statuses(): iterable
    {
        yield 'relative to the root' => ['{"config-sync":"../config/sync","root":"/p/web"}', '/p/web/../config/sync'];
        yield 'absolute' => ['{"config-sync":"/config/sync","root":"/p/web"}', '/config/sync'];
    }

    #[\PHPUnit\Framework\Attributes\DataProvider('statuses')]
    public function testDrushNamesTheConfigSyncDirectoryAbsolute(string $status, string $directory): void
    {
        $runner = new FakeProcessRunner([new ProcessResult(0, $status, '')]);

        self::assertSame($directory, (new Drush($runner, new Bin('/bin'), '/p'))->configSyncDir('a'));
        self::assertSame(['/bin/drush', 'status', '--fields=config-sync,root', '--format=json'], $runner->runs[0]['command']);
    }

    /**
     * @return iterable<string, array{string}>
     */
    public static function statusesWithoutSync(): iterable
    {
        yield 'missing' => ['{"root":"/p/web"}'];
        yield 'empty' => ['{"config-sync":"","root":"/p/web"}'];
        yield 'not a string' => ['{"config-sync":["x"],"root":"/p/web"}'];
        yield 'not an object' => ['[]'];
    }

    #[\PHPUnit\Framework\Attributes\DataProvider('statusesWithoutSync')]
    public function testASiteWithoutAConfigSyncDirectoryIsAFailure(string $status): void
    {
        self::assertFailure('site a has no config sync directory', static fn () => (new Drush(new FakeProcessRunner([new ProcessResult(0, $status, 'x')]), new Bin('/bin'), '/p'))->configSyncDir('a'));
    }

    public function testDrushIncludesTheCommandsTheAgentShips(): void
    {
        self::assertFileExists(Drush::include() . '/Commands/DrupdaterCommands.php');
    }

    public function testComposerNormalizeRunsComposerAgainAsAScriptsComposerWould(): void
    {
        $runner = new FakeProcessRunner([new ProcessResult(0, '', ''), new ProcessResult(1, '', 'invalid')]);
        $composer = new ComposerCli($runner, '/usr/bin/composer.phar', '/p');

        $composer->normalize();
        self::assertSame([PHP_BINARY, '/usr/bin/composer.phar', 'normalize', '--no-interaction'], $runner->runs[0]['command']);

        self::assertFailure('composer normalize failed', $composer->normalize(...));
    }

    public function testTheProcessRunnerKeepsTheStreamsApartAndAddsToTheEnvironment(): void
    {
        $result = (new ProcessRunner())->run(['sh', '-c', 'echo "$GREETING"; echo noise >&2; exit 3'], $this->scratchDir(), ['GREETING' => 'hello']);

        self::assertSame([3, "hello\n", "noise\n"], [$result->exitCode, $result->stdout, $result->stderr]);
        self::assertFalse($result->succeeded());
    }

    private static function assertFailure(string $reason, \Closure $run): void
    {
        try {
            $run();
            self::fail('no failure');
        }
        catch (Failure $failure) {
            self::assertSame([Failure::FAILED, $reason], [$failure->kind, $failure->reason]);
            self::assertNotSame('', $failure->getMessage(), 'the tool\'s own output says why');
        }
    }
}
