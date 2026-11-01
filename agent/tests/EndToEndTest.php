<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests;

use Composer\Util\Filesystem;
use PHPUnit\Framework\TestCase;
use Symfony\Component\Process\Process;

/**
 * Installs the agent as a global plugin, as the image does, and asks a real Composer.
 */
final class EndToEndTest extends TestCase
{
    use Scratch;

    private static string $home;

    public static function setUpBeforeClass(): void
    {
        self::$home = sys_get_temp_dir() . '/drupdater-agent-home-' . bin2hex(random_bytes(6));
        mkdir(self::$home);
        file_put_contents(self::$home . '/composer.json', json_encode([
            'repositories' => [
                ['type' => 'path', 'url' => dirname(__DIR__), 'options' => ['symlink' => true, 'versions' => ['drupdater/agent' => '1.0.0']]],
                ['packagist.org' => false],
            ],
            'require' => ['drupdater/agent' => '1.0.0'],
            'config' => ['allow-plugins' => ['drupdater/agent' => true]],
        ]));
        $install = self::composer(['global', 'update', '--no-interaction', '--no-progress']);
        self::assertSame(0, $install->getExitCode(), $install->getErrorOutput());
    }

    public static function tearDownAfterClass(): void
    {
        (new Filesystem())->removeDirectory(self::$home);
    }

    public function testHandshake(): void
    {
        $reply = $this->ask('drupdater:handshake', $this->project(), ['protocol' => 1, 'request' => []]);

        self::assertEquals(self::fixture('handshake/response.json'), $reply);
    }

    public function testAPatchThatFitsApplies(): void
    {
        $project = $this->project();
        $this->writeFiles($project, ['patches/greeting.patch' => self::GREETING_PATCH]);

        $reply = $this->ask('drupdater:patch-check', $project, $this->request([$project . '/patches/greeting.patch']));

        self::assertSame(['outcome' => 'applies'], $reply['result']);
    }

    public function testAPatchThatDoesNotFitIsRejected(): void
    {
        $project = $this->project();
        $this->writeFiles($project, ['patches/greeting.patch' => str_replace('hello', 'bonjour', self::GREETING_PATCH)]);

        $reply = $this->ask('drupdater:patch-check', $project, $this->request([$project . '/patches/greeting.patch']));

        self::assertSame(['outcome' => 'rejected', 'patch' => $project . '/patches/greeting.patch'], $reply['result']);
    }

    public function testAVersionNoRepositoryHasIsUnobtainable(): void
    {
        $project = $this->project();
        $this->writeFiles($project, ['patches/greeting.patch' => self::GREETING_PATCH]);
        $request = $this->request([$project . '/patches/greeting.patch']);
        $request['request']['version'] = '2.0.0';

        $reply = $this->ask('drupdater:patch-check', $project, $request);

        self::assertFalse($reply['ok']);
        self::assertSame(['unobtainable', 'not available from any configured repository'], [$reply['error']['kind'], $reply['error']['reason']]);
    }

    /**
     * @param array<string, mixed> $request
     * @return array<string, mixed>
     */
    private function ask(string $command, string $project, array $request): array
    {
        $process = self::composer(['--working-dir=' . $project, $command, '--no-interaction', '--no-ansi'], json_encode($request));
        $lines = explode("\n", trim($process->getOutput()));
        return json_decode(end($lines), true, 512, JSON_THROW_ON_ERROR);
    }

    /**
     * @param list<string> $patches
     * @return array<string, mixed>
     */
    private function request(array $patches): array
    {
        return ['protocol' => 1, 'request' => ['package' => 'fixture/greeting', 'version' => '1.0.0', 'reference' => '', 'patches' => $patches]];
    }

    private function project(): string
    {
        $package = $this->scratchDir();
        $this->writeFiles($package, self::PACKAGE_FILES + [
            'composer.json' => json_encode(['name' => 'fixture/greeting', 'version' => '1.0.0']),
        ]);
        $project = $this->scratchDir();
        $this->writeFiles($project, ['composer.json' => json_encode([
            'name' => 'fixture/project',
            'repositories' => [
                ['type' => 'path', 'url' => $package, 'options' => ['symlink' => false]],
                ['packagist.org' => false],
            ],
        ])]);
        return $project;
    }

    /**
     * @param list<string> $args
     */
    private static function composer(array $args, ?string $stdin = null): Process
    {
        $process = new Process(
            [PHP_BINARY, dirname(__DIR__) . '/vendor/bin/composer', ...$args],
            null,
            ['COMPOSER_HOME' => self::$home, 'COMPOSER_ALLOW_SUPERUSER' => '1', 'COMPOSER_CACHE_DIR' => self::$home . '/cache'],
            $stdin,
            120,
        );
        $process->run();
        return $process;
    }
}
