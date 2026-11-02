<?php

declare(strict_types=1);

namespace Drupdater\App\Tests;

use Drupdater\App\Tests\Git\Repository;
use PHPUnit\Framework\TestCase;
use Symfony\Component\Process\Process;

/**
 * The binary as the Go side runs it: its commands wired to real git and a real agent.
 */
final class ApplicationTest extends TestCase
{
    public function testCommitsIntoTheWorkingDirectoryByTheAuthor(): void
    {
        $repo = new Repository(['config/sync/a.yml' => 'a']);
        $repo->write('config/sync/a.yml', 'changed');

        $out = self::drupdater($repo->dir, 'internal:commit', ['author' => ['name' => 'Bot', 'email' => 'bot@example.com'], 'commits' => [['message' => 'Update configuration default', 'paths' => ['config/sync']]]]);

        self::assertSame('{"protocol":6,"command":"internal:commit","ok":true,"result":{"committed":["Update configuration default"]}}' . "\n", $out);
        self::assertSame(['Update configuration default', ['config/sync/a.yml']], $repo->head());
        self::assertSame('Bot <bot@example.com>', $repo->git('log', '-1', '--format=%an <%ae>'));
    }

    public function testFiresAnEventNoAddonHooks(): void
    {
        $repo = new Repository();

        $out = self::drupdater($repo->dir, 'internal:fire', [
            'event' => ['name' => 'post-code-update'],
            'addons' => [['name' => 'a', 'hooks' => ['pre-site-update' => 'inspect']]],
            'run' => ['security' => false],
            'author' => ['name' => 'n', 'email' => 'e'],
        ]);

        self::assertSame('{"protocol":6,"command":"internal:fire","ok":true,"result":{"recorded":[],"plan":null,"abort":"","failure":""}}' . "\n", $out);
    }

    /**
     * @param array<string, mixed> $request
     */
    private static function drupdater(string $dir, string $command, array $request): string
    {
        $process = new Process([PHP_BINARY, __DIR__ . '/../bin/drupdater', $command, '--no-interaction', '--no-ansi'], $dir, null, json_encode(['protocol' => 6, 'request' => $request]));
        $process->mustRun();
        return $process->getOutput();
    }
}
