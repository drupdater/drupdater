<?php

declare(strict_types=1);

namespace Drupdater\App\Tests\Command;

use Symfony\Component\Console\Command\Command;
use Symfony\Component\Console\Tester\CommandTester;

/**
 * A command run with a request body on stdin, as the binary runs it.
 */
final class Run
{
    public static function command(Command $command, string $body): CommandTester
    {
        $tester = new CommandTester($command);
        $tester->setInputs([$body]);
        $tester->execute([]);
        return $tester;
    }

    /**
     * The fixture's JSON, compacted to the one line a command prints.
     */
    public static function fixture(string $name): string
    {
        $decoded = json_decode((string) file_get_contents(__DIR__ . '/../../protocol/' . $name), false, 512, JSON_THROW_ON_ERROR);
        return (string) json_encode($decoded, JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE);
    }

    /**
     * @param array<string, mixed> $request
     */
    public static function request(array $request): string
    {
        return (string) json_encode(['protocol' => 6, 'request' => $request]);
    }
}
