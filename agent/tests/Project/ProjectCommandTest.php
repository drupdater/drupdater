<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\Project;

use Composer\Console\Application;
use Composer\Factory;
use Composer\IO\NullIO;
use Composer\Util\Filesystem;
use Drupdater\Agent\Command\ProjectCommand;
use Drupdater\Agent\Tests\Scratch;
use PHPUnit\Framework\TestCase;
use Symfony\Component\Console\Tester\CommandTester;

final class ProjectCommandTest extends TestCase
{
    use Scratch {
        tearDown as scratchTearDown;
    }

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

    public function testAnswersTheFixtureForTheFixtureProject(): void
    {
        [$status, $reply] = $this->ask($this->fixtureProject());

        self::assertSame(0, $status);
        self::assertEquals(self::fixture('project/response.json'), $reply);
    }

    public function testKeepsAnEmptyObjectAnObject(): void
    {
        $tester = $this->tester($this->fixtureProject());
        $tester->setInputs([json_encode(self::fixture('project/request.json'))]);
        $tester->execute([]);

        self::assertStringContainsString('"drupal/empty":{}', $tester->getDisplay());
        self::assertStringNotContainsString('[]', $tester->getDisplay());
    }

    public function testAProjectWithoutALockHasNoPackages(): void
    {
        $project = $this->fixtureProject();
        unlink($project . '/composer.lock');

        [, $reply] = $this->ask($project);

        self::assertNull($reply['result']['lock_hash']);
        self::assertSame([], $reply['result']['packages']);
        self::assertSame([], $reply['result']['dependency_patches']);
        self::assertSame('web', $reply['result']['web_root']);
    }

    public function testLeavesOutWhatComposerJsonDoesNotSay(): void
    {
        $project = $this->scratchDir();
        $this->writeFiles($project, ['composer.json' => '{"name": "fixture/bare"}', 'web/modules/custom/x.module' => '']);

        [, $reply] = $this->ask($project);

        self::assertSame([
            'web_root' => null,
            'custom_code_directories' => [],
            'lock_hash' => null,
            'packages' => [],
            'dependency_patches' => [],
        ], $reply['result']);
    }

    public function testPassesAnAllowPluginsBooleanThrough(): void
    {
        $project = $this->scratchDir();
        $this->writeFiles($project, ['composer.json' => json_encode([
            'config' => ['allow-plugins' => false],
            'extra' => ['drupal-scaffold' => ['locations' => ['web-root' => 'docroot']]],
        ])]);
        mkdir($project . '/docroot/profiles/custom', 0777, true);

        [, $reply] = $this->ask($project);

        self::assertFalse($reply['result']['allow_plugins']);
        self::assertSame('docroot', $reply['result']['web_root']);
        self::assertSame(['docroot/profiles/custom'], $reply['result']['custom_code_directories']);
    }

    public function testAnEmptyWebRootIsNoWebRoot(): void
    {
        $project = $this->scratchDir();
        $this->writeFiles($project, ['composer.json' => '{"extra": {"drupal-scaffold": {"locations": {"web-root": ""}}}}']);

        [, $reply] = $this->ask($project);

        self::assertNull($reply['result']['web_root']);
    }

    /**
     * @return array{int, array<string, mixed>}
     */
    private function ask(string $project): array
    {
        $tester = $this->tester($project);
        $tester->setInputs([json_encode(self::fixture('project/request.json'))]);
        $status = $tester->execute([]);
        return [$status, json_decode($tester->getDisplay(), true, 512, JSON_THROW_ON_ERROR)];
    }

    private function tester(string $project): CommandTester
    {
        chdir($project);
        $command = new ProjectCommand();
        $command->setComposer((new Factory())->createComposer(new NullIO(), $project . '/composer.json', true, $project));
        $application = new Application();
        $application->setAutoExit(false);
        $application->add($command);
        return new CommandTester($command);
    }

    private function fixtureProject(): string
    {
        $project = $this->scratchDir();
        (new Filesystem())->copy(__DIR__ . '/../fixtures/project', $project);
        return $project;
    }
}
