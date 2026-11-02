<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\Command;

use Composer\Console\Application;
use Drupdater\Agent\Addon\Catalogue;
use Drupdater\Agent\Addon\CatalogueEntry;
use Drupdater\Agent\Addon\Findings;
use Drupdater\Agent\Command\AddonsCommand;
use Drupdater\Agent\Command\AgentCommand;
use Drupdater\Agent\Command\ReportCommand;
use Drupdater\Agent\Tests\Addon\ScriptedAddon;
use Drupdater\Agent\Tests\Scratch;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use Symfony\Component\Console\Tester\CommandTester;

/**
 * drupdater:addons and drupdater:report, the two commands that need no project.
 */
final class CatalogueCommandsTest extends TestCase
{
    use Scratch;

    protected function tearDown(): void
    {
        ScriptedAddon::$report = null;
    }

    public function testAddonsAnswersTheFixture(): void
    {
        [$status, $reply] = self::ask(new AddonsCommand(), self::fixture('addons/request.json'));

        self::assertSame(0, $status);
        self::assertEquals(self::fixture('addons/response.json'), $reply);
    }

    public function testReportAnswersTheFixture(): void
    {
        [$status, $reply] = self::askVerbatim(new ReportCommand(), 'report/request.json');

        self::assertSame(0, $status);
        self::assertEquals(self::fixture('report/response.json'), $reply);
    }

    public function testReportHandsEachAddonItsOwnFindingsAndLeavesOutAnEmptySection(): void
    {
        $seen = null;
        ScriptedAddon::$report = static function (Findings $findings) use (&$seen): mixed {
            $seen = $findings;
            return null;
        };

        [, $reply, $output] = self::ask(new ReportCommand(new Catalogue([new CatalogueEntry(ScriptedAddon::class)])), ['protocol' => 3, 'request' => ['addons' => [
            ['addon' => 'scripted', 'findings' => [['event' => 'post-code-update', 'site' => '', 'finding' => 7, 'committed' => ['c']]]],
        ]]]);

        self::assertSame(7, $seen?->shared(\Drupdater\Agent\Addon\EventName::PostCodeUpdate)?->finding());
        self::assertStringContainsString('"sections":{}', $output, 'no section at all is still an object');
        self::assertTrue($reply['ok']);
    }

    /**
     * @return iterable<string, array{array<string, mixed>, string}>
     */
    public static function invalidReportRequests(): iterable
    {
        yield 'no addons' => [[], 'addons must be a list'];
        yield 'an unknown addon' => [['addons' => [['addon' => 'nope', 'findings' => []]]], 'no addon "nope" is in the catalogue'];
        yield 'an addon twice' => [['addons' => [['addon' => 'composer_diff', 'findings' => []], ['addon' => 'composer_diff', 'findings' => []]]], 'composer_diff is listed twice'];
        yield 'findings that are no list' => [['addons' => [['addon' => 'composer_diff', 'findings' => 'x']]], 'findings must be a list'];
    }

    /**
     * @param array<string, mixed> $request
     */
    #[DataProvider('invalidReportRequests')]
    public function testReportRefusesAnInvalidRequest(array $request, string $reason): void
    {
        [$status, $reply] = self::ask(new ReportCommand(), ['protocol' => 3, 'request' => $request]);

        self::assertSame(1, $status);
        self::assertSame(['kind' => 'invalid_request', 'reason' => $reason, 'message' => ''], $reply['error']);
    }

    /**
     * @param array<string, mixed> $request
     * @return array{int, array<string, mixed>, string}
     */
    private static function ask(AgentCommand $command, array $request): array
    {
        return self::execute($command, json_encode($request, JSON_THROW_ON_ERROR));
    }

    /**
     * Sends a fixture's bytes, so its {} reach the command as {}.
     *
     * @return array{int, array<string, mixed>, string}
     */
    private static function askVerbatim(AgentCommand $command, string $fixture): array
    {
        return self::execute($command, (string) file_get_contents(__DIR__ . '/../../protocol/' . $fixture));
    }

    /**
     * @return array{int, array<string, mixed>, string}
     */
    private static function execute(AgentCommand $command, string $body): array
    {
        $application = new Application();
        $application->setAutoExit(false);
        $application->add($command);
        $tester = new CommandTester($command);
        $tester->setInputs([$body]);
        $status = $tester->execute([]);
        $output = trim($tester->getDisplay());
        return [$status, json_decode($output, true, 512, JSON_THROW_ON_ERROR), $output];
    }
}
