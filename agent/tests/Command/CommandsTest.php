<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\Command;

use Composer\Console\Application;
use Drupdater\Agent\Command\AgentCommand;
use Drupdater\Agent\Command\HandshakeCommand;
use Drupdater\Agent\CommandProvider;
use Drupdater\Agent\Plugin;
use Drupdater\Agent\Tests\Scratch;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use Symfony\Component\Console\Tester\CommandTester;

final class CommandsTest extends TestCase
{
    use Scratch;

    public function testThePluginProvidesEveryCommand(): void
    {
        self::assertSame([\Composer\Plugin\Capability\CommandProvider::class => CommandProvider::class], (new Plugin())->getCapabilities());
        $names = array_map(fn (AgentCommand $c) => $c->getName(), (new CommandProvider())->getCommands());
        self::assertSame([
            'drupdater:addons',
            'drupdater:handshake',
            'drupdater:hook',
            'drupdater:platform-check',
            'drupdater:project',
            'drupdater:report',
            'drupdater:resolve',
        ], $names);
    }

    public function testHandshakeAnswersTheFixture(): void
    {
        [$status, $reply] = $this->ask(new HandshakeCommand(), self::fixture('handshake/request.json'));

        self::assertSame(0, $status);
        self::assertEquals(self::fixture('handshake/response.json'), $reply);
    }

    public function testAMismatchedProtocolIsRefused(): void
    {
        $request = self::fixture('handshake/request.json');
        $request['protocol'] = 4;

        [$status, $reply] = $this->ask(new HandshakeCommand(), $request);

        self::assertSame(1, $status);
        self::assertEquals(self::fixture('handshake/protocol-mismatch.json'), $reply);
    }

    /**
     * @return iterable<string, array{string, string}>
     */
    public static function unreadableBodies(): iterable
    {
        yield 'not JSON' => ['{', 'the request is not JSON'];
        yield 'no request object' => ['{"protocol":3}', 'the request has no request object'];
        yield 'not an object' => ['"x"', 'the request has no request object'];
    }

    #[DataProvider('unreadableBodies')]
    public function testAnUnreadableBodyIsAnInvalidRequest(string $body, string $reason): void
    {
        $tester = self::tester(new HandshakeCommand());
        $tester->setInputs([$body]);

        self::assertSame(1, $tester->execute([]));
        $reply = json_decode($tester->getDisplay(), true);
        self::assertSame('invalid_request', $reply['error']['kind']);
        self::assertSame($reason, $reply['error']['reason']);
    }

    public function testTheReplyIsTheOutputsOnlyLine(): void
    {
        $tester = self::tester(new HandshakeCommand());
        $tester->setInputs([json_encode(self::fixture('handshake/request.json'))]);
        $tester->execute([]);

        self::assertSame(1, substr_count(rtrim($tester->getDisplay()), "\n") + 1);
    }

    /**
     * @param array<string, mixed> $request
     * @return array{int, array<string, mixed>}
     */
    private function ask(AgentCommand $command, array $request): array
    {
        $tester = self::tester($command);
        $tester->setInputs([json_encode($request, JSON_THROW_ON_ERROR)]);
        $status = $tester->execute([]);
        return [$status, json_decode($tester->getDisplay(), true, 512, JSON_THROW_ON_ERROR)];
    }

    private static function tester(AgentCommand $command): CommandTester
    {
        $application = new Application();
        $application->setAutoExit(false);
        $application->add($command);
        return new CommandTester($command);
    }
}
