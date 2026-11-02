<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\Site;

use Drupdater\Agent\Failure;
use Drupdater\Agent\Log;
use Drupdater\Agent\Process\ProcessResult;
use Drupdater\Agent\Site\TestDatabase;
use Drupdater\Agent\Tests\Fails;
use Drupdater\Agent\Tests\FakeProcessRunner;
use Drupdater\Agent\Tests\Scratch;
use Drupdater\Agent\Tools\Bin;
use Drupdater\Agent\Tools\Drush;
use PHPUnit\Framework\TestCase;

final class TestDatabaseTest extends TestCase
{
    use Fails;
    use Scratch;

    private const WITH_SQLITE = "module:\n  node: 0\n  sqlite: 0\ntheme:\n  stark: 0\nprofile: standard\n";
    private const WITHOUT_SQLITE = "module:\n  node: 0\n  standard: 1000\ntheme:\n  stark: 0\nprofile: standard\n";

    private string $project;
    private string $data;
    private string $settings;
    private string $coreExtension;
    private FakeProcessRunner $drush;
    private Log $log;

    public function testInstallingRunsDrushOnTheBlockThenPutsSettingsBack(): void
    {
        $this->site("<?php\n", self::WITH_SQLITE);
        $during = null;
        $this->drush = new FakeProcessRunner([$this->drushStatus(), function () use (&$during): ProcessResult {
            $during = file_get_contents($this->settings);
            return new ProcessResult(0, '', '');
        }]);

        $this->database()->install('site1');

        self::assertSame("<?php\n" . <<<PHP

            // Added by drupdater: test database settings.
            \$databases['default']['default'] = [
            	'database' => '{$this->data}/site1.sqlite',
            	'prefix' => '',
            	'driver' => 'sqlite',
            	'namespace' => 'Drupal\\\\sqlite\\\\Driver\\\\Database\\\\sqlite',
            	'autoload' => 'core/modules/sqlite/src/Driver/Database/sqlite/',
            ];
            \$settings['skip_permissions_hardening'] = TRUE;
            \$settings['file_private_path'] = '{$this->data}/private/site1';
            \$settings['hash_salt'] = 'changeme';

            PHP, $during, 'a project that uses sqlite itself keeps exporting it');
        self::assertSame("<?php\n", file_get_contents($this->settings));
        self::assertSame(['/p/vendor/bin/drush', '--existing-config', '--yes', 'site:install', '--sites-subdir=site1', '--verbose'], $this->drush->runs[1]['command']);
        self::assertSame("module:\n  node: 0\n  sqlite: 0\ntheme:\n  stark: 0\n", file_get_contents($this->coreExtension), 'the profile goes');
        self::assertSame(['installing site', 'configuring database', 'restoring settings'], array_column($this->log->entries(), 'message'));
    }

    public function testAProjectWithoutSqliteGetsItEnabledAndExcludedFromTheExport(): void
    {
        $this->site("<?php\n", self::WITHOUT_SQLITE);

        $this->database()->configure('site1');

        self::assertStringContainsString("  sqlite: 0\n", (string) file_get_contents($this->coreExtension));
        self::assertStringEndsWith(<<<'PHP'
            $settings['hash_salt'] = 'changeme';

            if (isset($settings['config_exclude_modules'])) {
            	$settings['config_exclude_modules'][] = 'sqlite';
            } else {
            	$settings['config_exclude_modules'] = ['sqlite'];
            }

            PHP, (string) file_get_contents($this->settings));
        self::assertSame(['configuring database', 'enabling sqlite module'], array_column($this->log->entries(), 'message'));
    }

    public function testReconfiguringAfterRestoreKeepsTheSqliteExclusion(): void
    {
        $this->site("<?php\n", self::WITHOUT_SQLITE);

        $this->database()->configure('site1');
        $this->database()->restore('site1');
        $this->database()->configure('site1');

        self::assertStringContainsString("\$settings['config_exclude_modules'] = ['sqlite'];", (string) file_get_contents($this->settings));
    }

    public function testConfiguringTwiceLeavesOneBlock(): void
    {
        $this->site("<?php\n", self::WITH_SQLITE);

        $this->database()->configure('site1');
        $this->database()->configure('site1');

        self::assertSame(1, substr_count((string) file_get_contents($this->settings), TestDatabase::MARKER));
    }

    public function testABlockAKilledRunLeftBehindIsReplaced(): void
    {
        $this->site("<?php\n\n" . TestDatabase::MARKER . "\n\$databases['default']['default'] = ['database' => '/gone/site1.sqlite'];\n", self::WITHOUT_SQLITE);

        $this->database()->configure('site1');

        $settings = (string) file_get_contents($this->settings);
        self::assertSame(1, substr_count($settings, TestDatabase::MARKER));
        self::assertStringNotContainsString('/gone/', $settings);
        self::assertStringContainsString($this->data . '/site1.sqlite', $settings);
    }

    public function testRestoringPutsBackTheProjectsFileExactly(): void
    {
        $original = "<?php\n\n\$settings['x'] = 1;\n";
        $this->site($original, self::WITH_SQLITE);

        $this->database()->configure('site1');
        $this->database()->restore('site1');

        self::assertSame($original, file_get_contents($this->settings));
    }

    public function testRestoringAFileWithoutTheBlockLeavesItAlone(): void
    {
        $this->site('<?php', self::WITH_SQLITE);
        chmod($this->settings, 0444);

        $this->database()->restore('site1');

        self::assertSame('<?php', file_get_contents($this->settings));
        self::assertSame([], $this->log->entries());
    }

    /**
     * @return iterable<string, array{string, string}>
     */
    public static function blocks(): iterable
    {
        yield 'after a blank line' => ["<?php\n\n" . TestDatabase::MARKER . "\n\$x = 1;\n", "<?php\n"];
        yield 'only its own newline' => ["<?php\n\n\n" . TestDatabase::MARKER . "\n", "<?php\n\n"];
        yield 'right after the opening tag' => ["<?php\n" . TestDatabase::MARKER . "\n", '<?php'];
        yield 'alone' => [TestDatabase::MARKER . "\n", ''];
        yield 'after anything but a newline' => ['<?php' . TestDatabase::MARKER . "\n", '<?php'];
    }

    #[\PHPUnit\Framework\Attributes\DataProvider('blocks')]
    public function testRestoringCutsAtTheMarkerWithTheNewlineItStartsWith(string $settings, string $restored): void
    {
        $this->site($settings, self::WITH_SQLITE);

        $this->database()->restore('site1');

        self::assertSame($restored, file_get_contents($this->settings));
    }

    public function testAFailedInstallStillPutsSettingsBack(): void
    {
        $this->site("<?php\n", self::WITH_SQLITE);
        $this->drush = new FakeProcessRunner([$this->drushStatus(), new ProcessResult(1, '', 'no profile')]);

        self::assertFails(Failure::FAILED, 'drush --existing-config --yes site:install --sites-subdir=site1 --verbose failed', fn () => $this->database()->install('site1'));
        self::assertSame("<?php\n", file_get_contents($this->settings));
    }

    public function testAFailedInstallThatCannotPutSettingsBackSaysBoth(): void
    {
        $this->site("<?php\n", self::WITH_SQLITE);
        $this->drush = new FakeProcessRunner([$this->drushStatus(), function (): ProcessResult {
            unlink($this->settings);
            return new ProcessResult(1, '', 'no profile');
        }]);

        $failure = self::assertFails(Failure::FAILED, 'drush --existing-config --yes site:install --sites-subdir=site1 --verbose failed', fn () => $this->database()->install('site1'));
        self::assertStringEndsWith('; then failed to read settings file ' . $this->settings, $failure->getMessage());
        self::assertStringContainsString('no profile', $failure->getMessage());
    }

    public function testASiteWithoutSettingsIsRefusedAndNoneIsCreated(): void
    {
        $this->site("<?php\n", self::WITH_SQLITE);
        unlink($this->settings);

        self::assertFails(Failure::FAILED, 'failed to read settings file ' . $this->settings, fn () => $this->database()->configure('site1'));
        self::assertFileDoesNotExist($this->settings);
    }

    public function testAProjectWithoutAWebRootIsRefused(): void
    {
        $this->site("<?php\n", self::WITH_SQLITE);

        self::assertFails(Failure::FAILED, 'failed to get Drupal web dir: extra.drupal-scaffold.locations.web-root is not defined', fn () => $this->database(null)->restore('site1'));
    }

    public function testABlockThatCannotBeKeptIsAFailure(): void
    {
        $this->site("<?php\n", self::WITH_SQLITE);
        mkdir($this->data . '/site1.settings.php');

        self::assertFails(Failure::FAILED, 'failed to write ' . $this->data . '/site1.settings.php', fn () => $this->database()->configure('site1'));
        self::assertSame("<?php\n", file_get_contents($this->settings));
    }

    private function site(string $settings, string $coreExtension): void
    {
        $this->project = $this->scratchDir();
        $this->data = $this->scratchDir();
        $this->settings = $this->project . '/web/sites/site1/settings.php';
        $this->coreExtension = $this->project . '/config/sync/core.extension.yml';
        $this->writeFiles($this->project, ['web/sites/site1/settings.php' => $settings, 'config/sync/core.extension.yml' => $coreExtension]);
        $this->drush = new FakeProcessRunner(array_fill(0, 3, fn (): ProcessResult => $this->drushStatus()));
        $this->log = new Log();
    }

    private function drushStatus(): ProcessResult
    {
        return new ProcessResult(0, (string) json_encode(['config-sync' => '../config/sync', 'root' => $this->project . '/web'], JSON_THROW_ON_ERROR), '');
    }

    private function database(?string $webRoot = 'web'): TestDatabase
    {
        return new TestDatabase(new Drush($this->drush, new Bin('/p/vendor/bin'), $this->project), $this->log, $this->project, $webRoot, $this->data);
    }
}
