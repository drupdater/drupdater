<?php

declare(strict_types=1);

namespace Drupdater\Agent;

/**
 * The envelope every command reads and writes. Keep in step with internal/agent/agent.go.
 */
final class Protocol
{
    public const VERSION = 2;

    /**
     * @return array<string, mixed>
     */
    public static function decodeRequest(string $body): array
    {
        try {
            $decoded = json_decode($body, true, 512, JSON_THROW_ON_ERROR);
        }
        catch (\JsonException $e) {
            throw new Failure(Failure::INVALID_REQUEST, 'the request is not JSON', $e->getMessage(), $e);
        }
        if (!is_array($decoded) || !is_array($decoded['request'] ?? null)) {
            throw new Failure(Failure::INVALID_REQUEST, 'the request has no request object');
        }
        if (($decoded['protocol'] ?? null) !== self::VERSION) {
            throw new Failure(
                Failure::PROTOCOL_MISMATCH,
                sprintf('drupdater speaks protocol %s, the agent speaks %d', json_encode($decoded['protocol'] ?? null), self::VERSION),
            );
        }
        return $decoded['request'];
    }

    /**
     * @param array<string, mixed> $result
     */
    public static function success(string $command, array $result): string
    {
        return self::encode(['protocol' => self::VERSION, 'command' => $command, 'ok' => true, 'result' => (object) $result]);
    }

    public static function failure(string $command, Failure $failure): string
    {
        return self::encode([
            'protocol' => self::VERSION,
            'command' => $command,
            'ok' => false,
            'error' => ['kind' => $failure->kind, 'reason' => $failure->reason, 'message' => $failure->getMessage()],
        ]);
    }

    /**
     * @param array<string, mixed> $envelope
     */
    private static function encode(array $envelope): string
    {
        // One line: internal/agent reads the last line of stdout.
        return json_encode($envelope, JSON_THROW_ON_ERROR | JSON_UNESCAPED_SLASHES | JSON_INVALID_UTF8_SUBSTITUTE);
    }
}
