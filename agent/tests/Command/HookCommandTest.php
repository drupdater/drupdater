<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\Command;

use Composer\Console\Application;
use Drupdater\Agent\Addon\AbortRun;
use Drupdater\Agent\Addon\Catalogue;
use Drupdater\Agent\Addon\CatalogueEntry;
use Drupdater\Agent\Addon\Commit;
use Drupdater\Agent\Addon\Event;
use Drupdater\Agent\Addon\EventName;
use Drupdater\Agent\Addon\HookResult;
use Drupdater\Agent\Addon\Plan;
use Drupdater\Agent\Addon\Services;
use Drupdater\Agent\Command\HookCommand;
use Drupdater\Agent\Failure;
use Drupdater\Agent\Tests\Addon\ScriptedAddon;
use Drupdater\Agent\Tests\InProject;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use Symfony\Component\Console\Tester\CommandTester;

final class HookCommandTest extends TestCase
{
    use InProject {
        tearDown as projectTearDown;
    }

    private const LOCKED = [
        'packages' => [
            ['name' => 'fixture/a', 'version' => '1.1.0', 'type' => 'library', 'source' => ['type' => 'git', 'url' => 'https://github.com/fixture/a.git', 'reference' => '1.1.0']],
            ['name' => 'fixture/new', 'version' => '3.0.0', 'type' => 'library'],
        ],
        'packages-dev' => [
            ['name' => 'fixture/tool', 'version' => '2.0.0', 'type' => 'library', 'source' => ['type' => 'git', 'url' => 'https://github.com/fixture/tool.git', 'reference' => '2.0.0']],
        ],
    ];

    protected function tearDown(): void
    {
        ScriptedAddon::$hook = null;
        $this->projectTearDown();
    }

    public function testComposerDiffAnswersTheFixture(): void
    {
        [$status, $reply] = $this->ask(new HookCommand(), self::fixture('hook/request.json'));

        self::assertSame(0, $status);
        self::assertEquals(self::fixture('hook/response.json'), $reply);
    }

    public function testTheHookSeesItsEventItsEarlierFindingsAndTheRun(): void
    {
        $seen = null;
        ScriptedAddon::$hook = static function (Event $event) use (&$seen): HookResult {
            $seen = $event;
            return new HookResult(['saw' => $event->earlier->shared(EventName::PreComposerUpdate)?->finding(false)]);
        };

        [, , $output] = $this->ask($this->scripted(), self::request(['name' => 'post-code-update'], [
            ['event' => 'pre-composer-update', 'site' => '', 'finding' => ['empty' => new \stdClass()], 'committed' => ['x']],
        ], security: true));

        self::assertInstanceOf(Event::class, $seen);
        self::assertSame(EventName::PostCodeUpdate, $seen->name);
        self::assertTrue($seen->security);
        self::assertSame(realpath($this->project), $seen->projectDir);
        self::assertStringContainsString('"finding":{"saw":{"empty":{}}}', $output, 'an empty object reads back as one');
    }

    public function testRepliesWithTheCommitsAndThePlanAHookReturns(): void
    {
        ScriptedAddon::$hook = static fn (): HookResult => new HookResult(['n' => 1], [new Commit('Change it', ['a', 'b'])], new Plan(['x/y'], minimalChanges: true));

        [, $reply] = $this->ask($this->scripted(), self::request(['name' => 'pre-composer-update', 'plan' => []]));

        self::assertSame([
            'finding' => ['n' => 1],
            'commits' => [['message' => 'Change it', 'paths' => ['a', 'b']]],
            'plan' => ['only' => ['x/y'], 'keep' => [], 'minimal_changes' => true],
            'abort' => null,
        ], $reply['result']);
    }

    public function testAnAbortedRunIsAnAnswerNotAFailure(): void
    {
        ScriptedAddon::$hook = static fn (): HookResult => throw new AbortRun('nothing to do');

        [$status, $reply] = $this->ask($this->scripted(), self::request(['name' => 'pre-composer-update', 'plan' => []]));

        self::assertSame(0, $status);
        self::assertSame(['finding' => null, 'commits' => [], 'plan' => null, 'abort' => 'nothing to do'], $reply['result']);
    }

    public function testWhatAFailingHookLoggedStillReachesTheEnvelope(): void
    {
        ScriptedAddon::$hook = static function (Event $event, Services $services): HookResult {
            $services->log->warning('about to fail', ['why' => 'test']);
            throw new Failure(Failure::FAILED, 'it failed');
        };

        [$status, $reply] = $this->ask($this->scripted(), self::request(['name' => 'post-code-update']));

        self::assertSame(1, $status);
        self::assertSame(['kind' => 'failed', 'reason' => 'it failed', 'message' => ''], $reply['error']);
        self::assertSame([['level' => 'warning', 'message' => 'about to fail', 'context' => ['why' => 'test', 'addon' => 'scripted']]], $reply['log']);
    }

    public function testAPlanOnAnyOtherEventIsTheAgentsOwnFault(): void
    {
        ScriptedAddon::$hook = static fn (): HookResult => new HookResult(plan: new Plan());

        [, $reply] = $this->ask($this->scripted(), self::request(['name' => 'post-code-update']));

        self::assertSame('internal', $reply['error']['kind']);
        self::assertStringContainsString('proposed a plan on post-code-update', $reply['error']['message']);
    }

    /**
     * @return iterable<string, array{array<string, mixed>, string}>
     */
    public static function invalidRequests(): iterable
    {
        yield 'no addon' => [['event' => ['name' => 'post-code-update']], 'no addon null is in the catalogue'];
        yield 'an unknown addon' => [['addon' => 'nope', 'event' => []], 'no addon "nope" is in the catalogue'];
        yield 'no event' => [['addon' => 'scripted'], 'event must be an object'];
        yield 'an unknown event' => [['addon' => 'scripted', 'event' => ['name' => 'post-everything']], 'unknown event "post-everything"'];
        yield 'an event it has no hook for' => [['addon' => 'scripted', 'event' => ['name' => 'post-site-update', 'site' => 'default', 'facts' => []]], 'scripted has no hook for post-site-update'];
        yield 'a security setting that is no boolean' => [['addon' => 'scripted', 'event' => ['name' => 'post-code-update'], 'run' => ['security' => 'yes']], 'run.security must be a boolean'];
        yield 'earlier findings that are no list' => [['addon' => 'scripted', 'event' => ['name' => 'post-code-update'], 'earlier' => ['a' => 1]], 'findings must be a list'];
        yield 'an earlier finding of an unknown event' => [
            ['addon' => 'scripted', 'event' => ['name' => 'post-code-update'], 'earlier' => [['event' => 'later', 'site' => '', 'finding' => null, 'committed' => []]]],
            'each finding must have a known event, a site, the finding and what it committed',
        ];
        yield 'a pre-composer-update without its plan' => [['addon' => 'scripted', 'event' => ['name' => 'pre-composer-update']], 'plan must be an object'];
    }

    /**
     * @param array<string, mixed> $request
     */
    #[DataProvider('invalidRequests')]
    public function testRefusesAnInvalidRequest(array $request, string $reason): void
    {
        ScriptedAddon::$hook = static fn (): HookResult => throw new \LogicException('the hook ran');

        [$status, $reply] = $this->ask($this->scripted(), ['protocol' => 3, 'request' => $request]);

        self::assertSame(1, $status);
        self::assertSame(['kind' => 'invalid_request', 'reason' => $reason, 'message' => ''], $reply['error']);
    }

    private string $project = '';

    private function scripted(): HookCommand
    {
        return new HookCommand(new Catalogue([new CatalogueEntry(ScriptedAddon::class)]));
    }

    /**
     * @param array<string, mixed> $event
     * @param list<array<string, mixed>> $earlier
     * @return array<string, mixed>
     */
    private static function request(array $event, array $earlier = [], bool $security = false): array
    {
        return ['protocol' => 3, 'request' => ['addon' => 'scripted', 'event' => $event, 'earlier' => $earlier, 'run' => ['security' => $security]]];
    }

    /**
     * @param array<string, mixed> $request
     * @return array{int, array<string, mixed>, string}
     */
    private function ask(HookCommand $command, array $request): array
    {
        $this->project = $this->scratchDir();
        $this->writeFiles($this->project, [
            'composer.json' => json_encode([
                'name' => 'fixture/site',
                'repositories' => [['packagist.org' => false]],
                'require' => ['fixture/a' => '^1.0'],
                'require-dev' => ['fixture/tool' => '^2.0'],
            ], JSON_UNESCAPED_SLASHES),
            'composer.lock' => json_encode(self::LOCKED, JSON_UNESCAPED_SLASHES),
        ]);
        $command->setComposer($this->composer($this->project));
        $application = new Application();
        $application->setAutoExit(false);
        $application->add($command);
        $tester = new CommandTester($command);
        $tester->setInputs([json_encode($request, JSON_THROW_ON_ERROR)]);
        $status = $tester->execute([]);
        $output = trim($tester->getDisplay());
        return [$status, json_decode($output, true, 512, JSON_THROW_ON_ERROR), $output];
    }
}
