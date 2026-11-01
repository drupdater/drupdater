<?php

declare(strict_types=1);

namespace Drupdater\Agent;

/**
 * What a command logs, handed back in its envelope for drupdater to log at the same level: the
 * agent's own output never reaches drupdater's log.
 */
final class Log
{
    public const DEBUG = 'debug';
    public const INFO = 'info';
    public const WARNING = 'warning';
    public const ERROR = 'error';

    /** @var list<array{level: string, message: string, context: array<string, mixed>}> */
    private array $entries = [];

    /**
     * @param array<string, mixed> $context added to every entry, under what the entry sets itself
     */
    public function __construct(private readonly array $context = [])
    {
    }

    /**
     * @param array<string, mixed> $context JSON-encodable values, by field name
     */
    public function debug(string $message, array $context = []): void
    {
        $this->add(self::DEBUG, $message, $context);
    }

    /**
     * @param array<string, mixed> $context
     */
    public function info(string $message, array $context = []): void
    {
        $this->add(self::INFO, $message, $context);
    }

    /**
     * @param array<string, mixed> $context
     */
    public function warning(string $message, array $context = []): void
    {
        $this->add(self::WARNING, $message, $context);
    }

    /**
     * @param array<string, mixed> $context
     */
    public function error(string $message, array $context = []): void
    {
        $this->add(self::ERROR, $message, $context);
    }

    /**
     * @return list<array{level: string, message: string, context: array<string, mixed>}>
     */
    public function entries(): array
    {
        return $this->entries;
    }

    /**
     * @param array<string, mixed> $context
     */
    private function add(string $level, string $message, array $context): void
    {
        $this->entries[] = ['level' => $level, 'message' => $message, 'context' => $context + $this->context];
    }
}
