<?php

declare(strict_types=1);

namespace Drupdater\App;

/**
 * What a command logs, handed back in its envelope for drupdater to log at the same level. The
 * agent's own log entries are passed on through it, as the agent logged them.
 */
final class Log
{
    public const DEBUG = 'debug';
    public const INFO = 'info';
    public const WARNING = 'warning';
    public const ERROR = 'error';

    private const LEVELS = [self::DEBUG, self::INFO, self::WARNING, self::ERROR];

    /** @var list<array{level: string, message: string, context: array<string, mixed>}> */
    private array $entries = [];

    /**
     * @param array<string, mixed> $context JSON-encodable values, by field name
     */
    public function debug(string $message, array $context = []): void
    {
        $this->add(self::DEBUG, $message, $context);
    }

    /**
     * An entry at a level read from elsewhere; one this log does not know reads as info.
     *
     * @param array<string, mixed> $context
     */
    public function add(string $level, string $message, array $context): void
    {
        $level = in_array($level, self::LEVELS, true) ? $level : self::INFO;
        $this->entries[] = ['level' => $level, 'message' => $message, 'context' => $context];
    }

    /**
     * @return list<array{level: string, message: string, context: array<string, mixed>}>
     */
    public function entries(): array
    {
        return $this->entries;
    }
}
