<?php

declare(strict_types=1);

namespace Drupdater\App\Tests\Agent;

use Drupdater\App\Agent\Agent;
use Drupdater\App\Json;
use Drupdater\App\Log;
use Drupdater\App\Pipeline\AgentHooks;
use Drupdater\App\Pipeline\Commit;
use Drupdater\App\Pipeline\Event;
use Drupdater\App\Pipeline\EventName;
use Drupdater\App\Pipeline\Finding;
use Drupdater\App\Pipeline\HookReply;
use Drupdater\App\Pipeline\Pin;
use Drupdater\App\Pipeline\UpdatePlan;
use PHPUnit\Framework\TestCase;

/**
 * Against agent/protocol/hook, the exchange the agent's own suite asserts it answers.
 */
final class AgentHooksTest extends TestCase
{
    private const FIXTURES = __DIR__ . '/../../../agent/protocol/hook/';

    public function testSendsTheAgentsHookRequestAndReadsItsReply(): void
    {
        $request = json_decode((string) file_get_contents(self::FIXTURES . 'request.json'), false, 512, JSON_THROW_ON_ERROR);
        $response = json_decode((string) file_get_contents(self::FIXTURES . 'response.json'), false, 512, JSON_THROW_ON_ERROR);
        $composer = new FakeComposer((string) json_encode($response));
        $event = Event::fromJson(Json::at($request, 'request', 'event'));

        $reply = (new AgentHooks(new Agent(new Log(), $composer->argv())))->run('/', 'composer_diff', $event->payload(new UpdatePlan()), [], false);

        self::assertEquals($request, json_decode($composer->seenStdin(), false));
        self::assertEquals(new HookReply(Json::at($response, 'result', 'finding')), $reply);
    }

    public function testSendsEarlierFindingsAndReadsCommitsAPlanAndAnAbort(): void
    {
        $composer = new FakeComposer((string) json_encode(['protocol' => 6, 'command' => 'drupdater:hook', 'ok' => true, 'result' => [
            'finding' => ['n' => 1],
            'commits' => [['message' => 'm', 'paths' => ['a', 'b']]],
            'plan' => ['only' => ['x/y'], 'keep' => [['package' => 'p/q', 'version' => '1.0.0']], 'minimal_changes' => true],
            'abort' => 'nothing to do',
        ]]));

        $reply = (new AgentHooks(new Agent(new Log(), $composer->argv())))->run('/', 'a', ['name' => 'pre-composer-update'], [new Finding(EventName::PreSiteUpdate, 's', (object) [], ['c'])], true);

        self::assertSame(
            '{"protocol":6,"request":{"addon":"a","event":{"name":"pre-composer-update"},"earlier":[{"event":"pre-site-update","site":"s","finding":{},"committed":["c"]}],"run":{"security":true}}}',
            $composer->seenStdin(),
        );
        self::assertEquals(new HookReply((object) ['n' => 1], [new Commit('m', ['a', 'b'])], new UpdatePlan(['x/y'], [new Pin('p/q', '1.0.0')], true), 'nothing to do'), $reply);
    }
}
