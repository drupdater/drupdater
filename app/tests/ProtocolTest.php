<?php

declare(strict_types=1);

namespace Drupdater\App\Tests;

use Drupdater\App\Failure;
use Drupdater\App\Protocol;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;

final class ProtocolTest extends TestCase
{
    public function testDecodesTheRequestKeepingObjectsAsObjects(): void
    {
        $request = Protocol::decodeRequest('{"protocol":6,"request":{"finding":{},"list":[]}}');

        self::assertSame('{"finding":{},"list":[]}', Protocol::encode($request));
    }

    /**
     * @return iterable<string, array{string, string, string}>
     */
    public static function refused(): iterable
    {
        yield 'not JSON' => ['{', Failure::INVALID_REQUEST, 'the request is not JSON'];
        yield 'no request' => ['{"protocol":6}', Failure::INVALID_REQUEST, 'the request has no request object'];
        yield 'a request that is a list' => ['{"protocol":6,"request":[]}', Failure::INVALID_REQUEST, 'the request has no request object'];
        yield 'another protocol' => ['{"protocol":5,"request":{}}', Failure::PROTOCOL_MISMATCH, 'drupdater speaks protocol 5, the app speaks 6'];
        yield 'no protocol' => ['{"request":{}}', Failure::PROTOCOL_MISMATCH, 'drupdater speaks protocol null, the app speaks 6'];
    }

    #[DataProvider('refused')]
    public function testRefuses(string $body, string $kind, string $reason): void
    {
        try {
            Protocol::decodeRequest($body);
            self::fail('accepted');
        }
        catch (Failure $failure) {
            self::assertSame([$kind, $reason], [$failure->kind, $failure->reason]);
        }
    }

    public function testALogEntrysContextIsAlwaysAnObject(): void
    {
        self::assertSame(
            '{"protocol":6,"command":"c","ok":true,"result":{"a":1},"log":[{"level":"info","message":"m","context":{}}]}',
            Protocol::success('c', ['a' => 1], [['level' => 'info', 'message' => 'm', 'context' => []]]),
        );
    }

    public function testEncodesOneLineUnescapedAsGoWould(): void
    {
        self::assertSame('{"url":"https://x/ä"}', Protocol::encode(['url' => "https://x/\u{e4}"]));
        self::assertSame('{"protocol":6,"command":"c","ok":true,"result":{}}', Protocol::success('c', [], []));
    }
}
