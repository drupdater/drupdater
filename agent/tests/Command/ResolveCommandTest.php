<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\Command;

use Composer\Console\Application;
use Drupdater\Agent\Command\ResolveCommand;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use Symfony\Component\Console\Tester\CommandTester;

final class ResolveCommandTest extends TestCase
{
    /**
     * @return iterable<string, array{array<string, mixed>, string}>
     */
    public static function invalidRequests(): iterable
    {
        yield 'packages not a list' => [['packages' => ['a' => 'x/y']], 'packages must be a list of strings'];
        yield 'a package not a string' => [['packages' => [1]], 'packages must be a list of strings'];
        yield 'keep not a list' => [['keep' => ['a' => []]], 'keep must be a list'];
        yield 'a pin without a version' => [['keep' => [['package' => 'x/y']]], 'each keep must have a package and a version'];
        yield 'a pin with an empty package' => [['keep' => [['package' => '', 'version' => '1.0.0']]], 'each keep must have a package and a version'];
        yield 'a pin with an empty version' => [['keep' => [['package' => 'x/y', 'version' => '']]], 'each keep must have a package and a version'];
        yield 'a pin that is not an object' => [['keep' => ['x/y:1.0.0']], 'each keep must have a package and a version'];
    }

    /**
     * @param array<string, mixed> $request
     */
    #[DataProvider('invalidRequests')]
    public function testRefusesAnInvalidRequestBeforeTouchingAnything(array $request, string $reason): void
    {
        $command = new ResolveCommand();
        $application = new Application();
        $application->setAutoExit(false);
        $application->add($command);
        $tester = new CommandTester($command);
        $tester->setInputs([json_encode(['protocol' => 1, 'request' => $request])]);

        self::assertSame(1, $tester->execute([]));
        $reply = json_decode($tester->getDisplay(), true);
        self::assertSame(['kind' => 'invalid_request', 'reason' => $reason, 'message' => ''], $reply['error']);
    }
}
