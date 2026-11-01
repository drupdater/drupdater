<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\Command;

use Composer\Downloader\TransportException;
use Composer\Console\Application;
use Composer\IO\NullIO;
use Composer\Util\HttpDownloader;
use Composer\Util\ProcessExecutor;
use Drupdater\Agent\Command\AgentCommand;
use Drupdater\Agent\Command\HandshakeCommand;
use Drupdater\Agent\Command\PatchCheckCommand;
use Drupdater\Agent\CommandProvider;
use Drupdater\Agent\PatchCheck\ComposerPackageFetcher;
use Drupdater\Agent\PatchCheck\PackageFetcher;
use Drupdater\Agent\PatchCheck\PatchApplier;
use Drupdater\Agent\PatchCheck\PatchChecker;
use Drupdater\Agent\PatchCheck\PatchFiles;
use Drupdater\Agent\Plugin;
use Drupdater\Agent\Tests\Scratch;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use Symfony\Component\Console\Tester\CommandTester;

final class CommandsTest extends TestCase
{
    use Scratch;

    private const REMOTE_PATCH = 'https://www.drupal.org/files/issues/3200000-8.patch';

    public function testThePluginProvidesEveryCommand(): void
    {
        self::assertSame([\Composer\Plugin\Capability\CommandProvider::class => CommandProvider::class], (new Plugin())->getCapabilities());
        $names = array_map(fn (AgentCommand $c) => $c->getName(), (new CommandProvider())->getCommands());
        self::assertSame([
            'drupdater:allow-plugins',
            'drupdater:audit',
            'drupdater:handshake',
            'drupdater:patch-check',
            'drupdater:platform-check',
            'drupdater:project',
            'drupdater:resolve',
            'drupdater:write-patches',
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
        $request['protocol'] = 3;

        [$status, $reply] = $this->ask(new HandshakeCommand(), $request);

        self::assertSame(1, $status);
        self::assertEquals(self::fixture('handshake/protocol-mismatch.json'), $reply);
    }

    /**
     * @return iterable<string, array{string, string}>
     */
    public static function outcomes(): iterable
    {
        yield 'every patch fits' => ['patch-check/applies.json', self::FAREWELL_PATCH];
        yield 'the second patch does not' => ['patch-check/rejected.json', str_replace('goodbye', 'adieu', self::FAREWELL_PATCH)];
    }

    #[DataProvider('outcomes')]
    public function testPatchCheckAnswersTheFixture(string $fixture, string $remotePatch): void
    {
        $project = $this->scratchDir();
        $this->writeFiles($project, ['patches/token/3123456-local.patch' => self::GREETING_PATCH]);
        $downloader = $this->createMock(HttpDownloader::class);
        $downloader->method('copy')->with(self::REMOTE_PATCH)
            ->willReturnCallback(fn (string $url, string $to) => file_put_contents($to, $remotePatch));

        [$status, $reply] = $this->ask(
            $this->patchCheck($this->packageFetcher(), $downloader),
            $this->request($project),
        );

        self::assertSame(0, $status);
        self::assertEquals(self::fixture($fixture), $reply);
    }

    public function testPatchCheckReportsAnUnobtainablePackage(): void
    {
        $e = new TransportException('The "https://packages.example.com/packages.json" file could not be downloaded (HTTP/2 401 )', 401);
        $e->setStatusCode(401);
        $fetcher = $this->createMock(PackageFetcher::class);
        $fetcher->method('fetch')->with('drupal/token', '1.15.0', '')->willThrowException(ComposerPackageFetcher::unobtainable($e));

        [$status, $reply] = $this->ask($this->patchCheck($fetcher), self::fixture('patch-check/request.json'));

        self::assertSame(1, $status);
        self::assertEquals(self::fixture('patch-check/unobtainable.json'), $reply);
    }

    public function testPatchCheckReportsAnUnexpectedErrorAsInternal(): void
    {
        $fetcher = $this->createMock(PackageFetcher::class);
        $fetcher->method('fetch')->willThrowException(new \LogicException('boom'));

        [$status, $reply] = $this->ask($this->patchCheck($fetcher), self::fixture('patch-check/request.json'));

        self::assertSame(1, $status);
        self::assertSame(['kind' => 'internal', 'reason' => 'the agent failed', 'message' => 'boom'], $reply['error']);
    }

    public function testPatchCheckPassesTheReferenceOn(): void
    {
        $fetcher = $this->createMock(PackageFetcher::class);
        $fetcher->expects(self::once())->method('fetch')->with('drupal/token', 'dev-1.x', 'abc123')
            ->willThrowException(new \Drupdater\Agent\Failure('unobtainable', 'stop here'));
        $request = self::fixture('patch-check/request.json');
        $request['request']['version'] = 'dev-1.x';
        $request['request']['reference'] = 'abc123';

        [, $reply] = $this->ask($this->patchCheck($fetcher), $request);

        self::assertSame('stop here', $reply['error']['reason']);
    }

    /**
     * @return iterable<string, array{array<string, mixed>, string}>
     */
    public static function invalidRequests(): iterable
    {
        $valid = ['package' => 'drupal/token', 'version' => '1.15.0', 'patches' => ['/p.patch']];
        yield 'no package' => [array_diff_key($valid, ['package' => 0]), 'package must be a non-empty string'];
        yield 'empty version' => [['version' => ''] + $valid, 'version must be a non-empty string'];
        yield 'reference not a string' => [['reference' => 1] + $valid, 'reference must be a non-empty string'];
        yield 'no patches' => [['patches' => []] + $valid, 'patches must be a non-empty list of strings'];
        yield 'patches not a list' => [['patches' => ['a' => '/p.patch']] + $valid, 'patches must be a non-empty list of strings'];
        yield 'a patch not a string' => [['patches' => ['/p.patch', 3]] + $valid, 'patches must be a non-empty list of strings'];
    }

    /**
     * @param array<string, mixed> $request
     */
    #[DataProvider('invalidRequests')]
    public function testPatchCheckRefusesAnInvalidRequest(array $request, string $reason): void
    {
        $fetcher = $this->createMock(PackageFetcher::class);
        $fetcher->expects(self::never())->method('fetch');

        [$status, $reply] = $this->ask($this->patchCheck($fetcher), ['protocol' => 2, 'request' => $request]);

        self::assertSame(1, $status);
        self::assertSame(['kind' => 'invalid_request', 'reason' => $reason, 'message' => ''], $reply['error']);
    }

    /**
     * @return iterable<string, array{string, string}>
     */
    public static function unreadableBodies(): iterable
    {
        yield 'not JSON' => ['{', 'the request is not JSON'];
        yield 'no request object' => ['{"protocol":2}', 'the request has no request object'];
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

    private function patchCheck(PackageFetcher $fetcher, ?HttpDownloader $downloader = null): PatchCheckCommand
    {
        $downloader ??= $this->createMock(HttpDownloader::class);
        return new PatchCheckCommand(fn () => new PatchChecker(
            $fetcher,
            new PatchFiles($downloader),
            new PatchApplier(new ProcessExecutor(new NullIO())),
        ));
    }

    private function packageFetcher(): PackageFetcher
    {
        $fetcher = $this->createMock(PackageFetcher::class);
        $fetcher->method('fetch')->with('drupal/token', '1.15.0', '')
            ->willReturnCallback(fn (string $n, string $v, string $r, string $target) => $this->writeFiles($target, self::PACKAGE_FILES));
        return $fetcher;
    }

    /**
     * @return array<string, mixed>
     */
    private function request(string $project): array
    {
        $request = self::fixture('patch-check/request.json');
        $request['request']['patches'] = str_replace('/project/', $project . '/', $request['request']['patches']);
        return $request;
    }
}
