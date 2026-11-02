<?php

declare(strict_types=1);

namespace Drupdater\App\Tests\Command;

use Drupdater\App\Agent\AgentError;
use Drupdater\App\Command\FireCommand;
use Drupdater\App\Git\Committer;
use Drupdater\App\Git\Identity;
use Drupdater\App\Log;
use Drupdater\App\Pipeline\Commit;
use Drupdater\App\Pipeline\HookReply;
use Drupdater\App\Pipeline\Hooks;
use Drupdater\App\Pipeline\Pin;
use Drupdater\App\Pipeline\UpdatePlan;
use Drupdater\App\Tests\Pipeline\RecordingCommitter;
use Drupdater\App\Tests\Pipeline\ScriptedHooks;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;

final class FireCommandTest extends TestCase
{
    /** @var list<Identity> */
    private array $authors = [];

    public function testAnswersTheProtocolExample(): void
    {
        $hooks = self::exampleHooks();

        $tester = Run::command($this->command($hooks, new RecordingCommitter(['Pin drupal/core'])), Run::fixture('fire/request.json'));

        self::assertSame(Run::fixture('fire/response.json') . "\n", $tester->getDisplay());
        self::assertSame(0, $tester->getStatusCode());
        self::assertEquals([new Identity('drupdater', 'drupdater@localhost')], $this->authors);
        self::assertSame(['composer_audit', 'composer_patches'], $hooks->called());
        self::assertSame((string) getcwd(), $hooks->calls[0]['dir']);
        self::assertTrue($hooks->calls[0]['security']);
        self::assertSame('{"event":"post-site-update","site":"default","finding":{},"committed":[]}', json_encode($hooks->calls[0]['earlier'][0]->toJson()), 'a finding passes through as written');
    }

    public function testAnswersAFailedEventWithWhatItRecordedFirst(): void
    {
        $hooks = new ScriptedHooks([
            'composer_audit' => static fn (): HookReply => new HookReply((object) ['advisories' => 1]),
            'composer_patches' => static fn (): HookReply => throw new AgentError('drupdater:hook', 'failed', 'the patch does not apply'),
        ]);

        $tester = Run::command($this->command($hooks, new RecordingCommitter()), Run::fixture('fire/request.json'));

        self::assertSame(Run::fixture('fire/failed.json') . "\n", $tester->getDisplay());
        self::assertSame(0, $tester->getStatusCode(), 'the command answered; the event failed');
    }

    /**
     * @return iterable<string, array{array<string, mixed>, string}>
     */
    public static function invalid(): iterable
    {
        $valid = ['event' => ['name' => 'post-code-update'], 'addons' => [], 'run' => ['security' => false], 'author' => ['name' => 'n', 'email' => 'e']];
        yield 'an unknown event' => [['event' => ['name' => 'later']] + $valid, 'unknown event "later"'];
        yield 'no event' => [['event' => null] + $valid, 'unknown event null'];
        yield 'an addon without a name' => [['addons' => [['hooks' => []]]] + $valid, 'an addon has no name'];
        yield 'an addon hooking an unknown event' => [['addons' => [['name' => 'a', 'hooks' => ['later' => 'change']]]] + $valid, 'addon a hooks the unknown event later'];
        yield 'an addon hooking at an unknown stage' => [['addons' => [['name' => 'a', 'hooks' => ['post-code-update' => 'soon']]]] + $valid, 'addon a hooks post-code-update at the unknown stage "soon"'];
        yield 'a finding of an unknown event' => [['addons' => [['name' => 'a', 'hooks' => [], 'findings' => [['event' => 'x']]]]] + $valid, 'a finding names the unknown event "x"'];
        yield 'security that is no boolean' => [['run' => ['security' => 'yes']] + $valid, 'run.security must be a boolean'];
        yield 'no author' => [['author' => null] + $valid, 'author needs a name and an email'];
        yield 'an author without an email' => [['author' => ['name' => 'n']] + $valid, 'author needs a name and an email'];
    }

    /**
     * @param array<string, mixed> $request
     */
    #[DataProvider('invalid')]
    public function testRefusesAnInvalidRequest(array $request, string $reason): void
    {
        $tester = Run::command($this->command(new ScriptedHooks(), new RecordingCommitter()), Run::request($request));

        self::assertSame(
            json_encode(['protocol' => 6, 'command' => 'internal:fire', 'ok' => false, 'error' => ['kind' => 'invalid_request', 'reason' => $reason, 'message' => '']], JSON_UNESCAPED_SLASHES) . "\n",
            $tester->getDisplay(),
        );
        self::assertSame(1, $tester->getStatusCode());
    }

    public function testSecurityDefaultsToANormalRun(): void
    {
        $hooks = new ScriptedHooks();

        Run::command($this->command($hooks, new RecordingCommitter()), Run::request([
            'event' => ['name' => 'post-code-update'],
            'addons' => [['name' => 'a', 'hooks' => ['post-code-update' => 'inspect']]],
            'author' => ['name' => 'n', 'email' => 'e'],
        ]));

        self::assertFalse($hooks->calls[0]['security']);
    }

    public function testAnythingElseIsAnInternalFailureCarryingTheLog(): void
    {
        $log = new Log();
        $log->debug('before', ['k' => 'v']);
        $hooks = new class implements Hooks {
            public function run(string $dir, string $addon, array $event, array $earlier, bool $security): HookReply
            {
                throw new \LogicException('a bug');
            }
        };
        $command = new FireCommand($log, $hooks, static fn (): Committer => new RecordingCommitter());

        $tester = Run::command($command, Run::request([
            'event' => ['name' => 'post-code-update'],
            'addons' => [['name' => 'a', 'hooks' => ['post-code-update' => 'inspect']]],
            'author' => ['name' => 'n', 'email' => 'e'],
        ]));

        self::assertSame(
            '{"protocol":6,"command":"internal:fire","ok":false,"error":{"kind":"internal","reason":"the app failed","message":"a bug"},"log":[{"level":"debug","message":"before","context":{"k":"v"}}]}' . "\n",
            $tester->getDisplay(),
        );
        self::assertSame(1, $tester->getStatusCode());
    }

    public function testIsHidden(): void
    {
        self::assertTrue($this->command(new ScriptedHooks(), new RecordingCommitter())->isHidden());
    }

    private function command(Hooks $hooks, Committer $committer): FireCommand
    {
        return new FireCommand(new Log(), $hooks, function (string $dir, Identity $author) use ($committer): Committer {
            $this->authors[] = $author;
            return $committer;
        });
    }

    private static function exampleHooks(): ScriptedHooks
    {
        return new ScriptedHooks([
            'composer_audit' => static fn (): HookReply => new HookReply((object) ['advisories' => 1], plan: new UpdatePlan(['drupal/core'], minimalChanges: true)),
            'composer_patches' => static fn (): HookReply => new HookReply(null, [new Commit('Pin drupal/core', ['composer.json'])], new UpdatePlan(keep: [new Pin('drupal/core', '10.3.1')])),
        ]);
    }
}
