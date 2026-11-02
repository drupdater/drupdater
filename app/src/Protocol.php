<?php

declare(strict_types=1);

namespace Drupdater\App;

/**
 * The envelope drupdater's parts exchange: the app reads one from the Go binary and answers with
 * one, and speaks it to the agent. Keep VERSION in step with agent/src/Protocol.php and
 * internal/agent/agent.go.
 */
final class Protocol
{
    public const VERSION = 6;

    /**
     * Objects stay objects, so a value the app only passes on, a finding or a site's facts, is
     * encoded again exactly as it came: decoded into arrays, {} would come back as [].
     */
    public static function decodeRequest(string $body): \stdClass
    {
        try {
            $decoded = json_decode($body, false, 512, JSON_THROW_ON_ERROR);
        }
        catch (\JsonException $e) {
            throw new Failure(Failure::INVALID_REQUEST, 'the request is not JSON', $e->getMessage(), $e);
        }
        $request = Json::at($decoded, 'request');
        if (!$request instanceof \stdClass) {
            throw new Failure(Failure::INVALID_REQUEST, 'the request has no request object');
        }
        $version = Json::at($decoded, 'protocol');
        if ($version !== self::VERSION) {
            throw new Failure(
                Failure::PROTOCOL_MISMATCH,
                sprintf('drupdater speaks protocol %s, the app speaks %d', self::encode($version), self::VERSION),
            );
        }
        return $request;
    }

    /**
     * @param array<string, mixed> $result
     * @param list<array{level: string, message: string, context: array<string, mixed>}> $log
     */
    public static function success(string $command, array $result, array $log): string
    {
        return self::encode(['protocol' => self::VERSION, 'command' => $command, 'ok' => true, 'result' => (object) $result] + self::log($log));
    }

    /**
     * @param list<array{level: string, message: string, context: array<string, mixed>}> $log
     */
    public static function failure(string $command, Failure $failure, array $log): string
    {
        return self::encode([
            'protocol' => self::VERSION,
            'command' => $command,
            'ok' => false,
            'error' => ['kind' => $failure->kind, 'reason' => $failure->reason, 'message' => $failure->getMessage()],
        ] + self::log($log));
    }

    /**
     * One line, unescaped as Go writes JSON: the reader takes the last line of stdout, and a value
     * passed through reads as if Go had encoded it.
     */
    public static function encode(mixed $value): string
    {
        return json_encode($value, JSON_THROW_ON_ERROR | JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_INVALID_UTF8_SUBSTITUTE);
    }

    /**
     * Left out when empty, as the agent leaves it out.
     *
     * @param list<array{level: string, message: string, context: array<string, mixed>}> $log
     * @return array{log?: list<array{level: string, message: string, context: \stdClass}>}
     */
    private static function log(array $log): array
    {
        if ($log === []) {
            return [];
        }
        return ['log' => array_map(
            static fn (array $entry): array => ['level' => $entry['level'], 'message' => $entry['message'], 'context' => (object) $entry['context']],
            $log,
        )];
    }
}
