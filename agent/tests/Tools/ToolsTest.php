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
