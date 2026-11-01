<?php

declare(strict_types=1);

namespace Drupdater\Agent;

/**
 * A failure the agent reports in its envelope. The kinds mirror internal/agent's Kind.
 */
final class Failure extends \RuntimeException
{
    public const UNOBTAINABLE = 'unobtainable';
    public const INVALID_REQUEST = 'invalid_request';
    public const PROTOCOL_MISMATCH = 'protocol_mismatch';
    public const INTERNAL = 'internal';
    /** Composer ran and reported failure, or the project cannot do what was asked. */
    public const FAILED = 'failed';

    public function __construct(
        public readonly string $kind,
        public readonly string $reason,
        string $message = '',
        ?\Throwable $previous = null,
    ) {
        parent::__construct($message, 0, $previous);
    }
}
