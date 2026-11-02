<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\Command;

use Composer\Console\Application;
use Drupdater\Agent\Addon\Catalogue;
use Drupdater\Agent\Addon\CatalogueEntry;
use Drupdater\Agent\Addon\Findings;
use Drupdater\Agent\Command\AddonsCommand;
use Drupdater\Agent\Command\AgentCommand;
use Drupdater\Agent\Command\ConfigCommand;
use Drupdater\Agent\Command\MergeRequestCommand;
use Drupdater\Agent\Command\ReportCommand;
use Drupdater\Agent\Json;
use Drupdater\Agent\Tests\Addon\ScriptedAddon;
use Drupdater\Agent\Tests\Decoded;
use Drupdater\Agent\Tests\Scratch;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use Symfony\Component\Console\Tester\CommandTester;

/**
 * drupdater:addons, drupdater:config, drupdater:report and drupdater:merge-request, the commands
 * that need no project.
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

    public function testConfigAnswersTheFixture(): void
    {
        $dir = '/tmp/drupdater-config-fixture';
        (new \Symfony\Component\Filesystem\Filesystem())->dumpFile($dir . '/.drupdater.yaml', "sites: [default, second]\ntimeout: 1h30m\nrun_types:\n  normal:\n    addons: [code_beautifier]\n  security:\n    auto_merge: true\n");
        try {
            [$status, $reply] = self::askVerbatim(new ConfigCommand(), 'config/request.json');
        }
        finally {
            (new \Symfony\Component\Filesystem\Filesystem())->remove($dir);
        }

        self::assertSame(0, $status);
        self::assertEquals(self::fixture('config/response.json'), $reply);
    }

    public function testConfigFailsAnInvalidFileWithWhatToFix(): void
    {
        $path = $this->scratchDir() . '/.drupdater.yaml';
        file_put_contents($path, "sites: []\n");

        [$status, $reply] = self::ask(new ConfigCommand(), ['protocol' => 6, 'request' => ['path' => $path]]);

        self::assertSame(1, $status);
        self::assertSame(['kind' => 'failed', 'reason' => 'in ' . $path . ': no sites configured: "sites" must list at least one Drupal site name', 'message' => ''], $reply['error']);
    }

    /**
     * @return iterable<string, array{mixed}>
     */
    public static function invalidConfigPaths(): iterable
    {
        yield 'missing' => [null];
        yield 'relative' => ['.drupdater.yaml'];
        yield 'not a string' => [1];
    }

    #[DataProvider('invalidConfigPaths')]
    public function testConfigNeedsAnAbsolutePath(mixed $path): void
    {
        [$status, $reply] = self::ask(new ConfigCommand(), ['protocol' => 6, 'request' => ['path' => $path]]);

        self::assertSame(1, $status);
        self::assertSame(['kind' => 'invalid_request', 'reason' => 'path must be an absolute path', 'message' => ''], $reply['error']);
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

        [, $reply, $output] = self::ask(new ReportCommand(new Catalogue([new CatalogueEntry(ScriptedAddon::class)])), ['protocol' => 6, 'request' => ['addons' => [
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
        [$status, $reply] = self::ask(new ReportCommand(), ['protocol' => 6, 'request' => $request]);

        self::assertSame(1, $status);
        self::assertSame(['kind' => 'invalid_request', 'reason' => $reason, 'message' => ''], $reply['error']);
    }

    public function testMergeRequestAnswersTheFixture(): void
    {
        [$status, $reply] = self::askVerbatim(new MergeRequestCommand(), 'merge-request/request.json');

        self::assertSame(0, $status);
        self::assertEquals(self::fixture('merge-request/response.json'), $reply);
    }

    public function testAReportWithoutAddonsGetsADescriptionWithoutSections(): void
    {
        [$status, $reply] = self::ask(new MergeRequestCommand(), ['protocol' => 6, 'request' => ['report' => ['mode' => 'normal', 'started_at' => '2026-07-25T09:00:00Z']]]);

        self::assertSame(0, $status);
        self::assertSame('July 2026: Drupal Maintenance Updates', Json::at($reply, 'result', 'title'));
        self::assertStringNotContainsString('##', Json::string(Json::at($reply, 'result', 'description')));
    }

    /**
     * @return iterable<string, array{array<string, mixed>, string}>
     */
    public static function invalidMergeRequestRequests(): iterable
    {
        yield 'no report' => [[], 'report must be an object'];
        yield 'a report that is no object' => [['report' => ['x']], 'report must be an object'];
        yield 'addons that are no object' => [['report' => ['started_at' => '2026-07-25T09:00:00Z', 'addons' => ['x']]], 'the report\'s addons must be an object'];
        yield 'no start' => [['report' => ['addons' => ['composer_diff' => []]]], 'the report\'s started_at must be an RFC 3339 time'];
        yield 'an unreadable section' => [['report' => ['started_at' => '2026-07-25T09:00:00Z', 'addons' => ['composer_diff' => 'x']]], 'the report\'s composer_diff section is unreadable: the section must be a list'];
    }

    /**
     * @param array<string, mixed> $request
     */
    #[DataProvider('invalidMergeRequestRequests')]
    public function testMergeRequestRefusesAnInvalidRequest(array $request, string $reason): void
    {
        [$status, $reply] = self::ask(new MergeRequestCommand(), ['protocol' => 6, 'request' => $request]);

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
        return [$status, Decoded::envelope($output), $output];
    }
}
