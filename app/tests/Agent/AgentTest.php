<?php

declare(strict_types=1);

namespace Drupdater\App\Tests\Agent;

use Drupdater\App\Agent\Agent;
use Drupdater\App\Agent\AgentError;
use Drupdater\App\Log;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;

final class AgentTest extends TestCase
{
    public function testSendsTheRequestInAnEnvelopeAndReturnsTheResult(): void
    {
        $composer = new FakeComposer("a plugin's chatter\n" . '{"protocol":6,"command":"drupdater:project","ok":true,"result":{"web_root":"web"}}' . "\n", 'progress');
        $log = new Log();

        $result = (new Agent($log, $composer->argv()))->call(sys_get_temp_dir(), 'drupdater:project', ['a' => 1]);

        self::assertEquals((object) ['web_root' => 'web'], $result);
        self::assertSame(['drupdater:project', '--no-interaction', '--no-ansi'], $composer->seenArgv());
        self::assertSame('{"protocol":6,"request":{"a":1}}', $composer->seenStdin());
        self::assertSame(realpath(sys_get_temp_dir()), $composer->seenCwd());
        self::assertSame([['level' => 'debug', 'message' => 'drupdater:project', 'context' => ['dir' => sys_get_temp_dir(), 'stderr' => 'progress']]], $log->entries());
    }

    public function testAnEmptyRequestIsSentAsAnObject(): void
    {
        $composer = new FakeComposer('{"protocol":6,"command":"drupdater:install","ok":true,"result":{}}');

        (new Agent(new Log(), $composer->argv()))->call(sys_get_temp_dir(), 'drupdater:install', []);

        self::assertSame('{"protocol":6,"request":{}}', $composer->seenStdin());
    }

    public function testPassesOnTheAgentsLogEntriesOnSuccessAndFailureAlike(): void
    {
        $log = new Log();
        $entries = '"log":[{"level":"warning","message":"w","context":{"addon":"a"}},{"level":"loud","message":"l"}]';
        $succeeding = new FakeComposer('{"protocol":6,"command":"c","ok":true,"result":{},' . $entries . '}');
        $failing = new FakeComposer('{"protocol":6,"command":"c","ok":false,' . $entries . '}', '', 1);
        (new Agent($log, $succeeding->argv()))->call('/', 'c', []);
        try {
            (new Agent($log, $failing->argv()))->call('/', 'c', []);
        }
        catch (AgentError) {
        }

        $agentEntries = array_values(array_filter($log->entries(), static fn (array $entry): bool => $entry['level'] !== 'debug'));
        self::assertSame(array_fill(0, 2, ['level' => 'warning', 'message' => 'w', 'context' => ['addon' => 'a']]), [$agentEntries[0], $agentEntries[2]]);
        self::assertSame(['level' => 'info', 'message' => 'l', 'context' => []], $agentEntries[1], 'an unknown level reads as info');
    }

    /**
     * @return iterable<string, array{string, string, int, string}>
     */
    public static function failures(): iterable
    {
        yield 'the agent reports a failure' => [
            '{"protocol":6,"command":"c","ok":false,"error":{"kind":"unobtainable","reason":"r","message":"m"}}', 'stderr', 1,
            'c: unobtainable (r): m, output: stderr',
        ];
        yield 'a failure without an error' => ['{"protocol":6,"command":"c","ok":false}', '', 1, 'c: internal'];
        yield 'no envelope' => ['Fatal error', 'boom', 255, 'c: internal (no reply envelope on stdout): exit code 255, output: boom'];
        yield 'nothing at all' => ['', '', 0, 'c: internal (no reply envelope on stdout): exit code 0'];
        yield 'another command\'s envelope' => ['{"protocol":6,"command":"other","ok":true,"result":{}}', '', 0, 'c: internal (no reply envelope on stdout): exit code 0'];
        yield 'another protocol' => ['{"protocol":7,"command":"c","ok":true,"result":{}}', '', 0, 'c: protocol_mismatch (agent speaks protocol 7, the app speaks 6)'];
        yield 'an answer then a failing exit' => ['{"protocol":6,"command":"c","ok":true,"result":{}}', 'late', 2, 'c: internal (answered but failed): exit code 2, output: late'];
        yield 'a result that is no object' => ['{"protocol":6,"command":"c","ok":true,"result":[1]}', '', 0, 'c: internal (the result is not an object)'];
    }

    #[DataProvider('failures')]
    public function testFailures(string $stdout, string $stderr, int $exit, string $want): void
    {
        $composer = new FakeComposer($stdout, $stderr, $exit);

        try {
            (new Agent(new Log(), $composer->argv()))->call('/', 'c', []);
            self::fail('no error');
        }
        catch (AgentError $e) {
            self::assertSame($want, $e->getMessage());
            self::assertSame('c', $e->command);
        }
    }

    public function testAnErrorKeepsItsParts(): void
    {
        $e = new AgentError('c', 'failed', 'r', 'm', 'o');

        self::assertSame(['c', 'failed', 'r', 'm', 'o'], [$e->command, $e->kind, $e->reason, $e->detail, $e->output]);
        self::assertSame('c: failed', (new AgentError('c', 'failed', ''))->getMessage());
    }
}
