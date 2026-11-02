<?php

declare(strict_types=1);

namespace Drupdater\App\Agent;

/**
 * A failure the agent reported, or a reply that could not be read as one. Its message reads as
 * internal/agent's Error does, so a log line looks the same whichever side called.
 */
final class AgentError extends \RuntimeException
{
    public function __construct(
        public readonly string $command,
        public readonly string $kind,
        public readonly string $reason,
        public readonly string $detail = '',
        public readonly string $output = '',
    ) {
        $text = sprintf('%s: %s', $command, $kind);
        if ($reason !== '') {
            $text .= " ($reason)";
        }
        if ($detail !== '') {
            $text .= ": $detail";
        }
        if ($output !== '') {
            $text .= ", output: $output";
        }
        parent::__construct($text);
    }
}
