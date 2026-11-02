<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tools;

use Drupdater\Agent\Failure;
use Drupdater\Agent\Process\ProcessResult;

/**
 * The failure of a tool an addon ran: it ran, and reported failure.
 */
final class ToolFailure
{
    public static function exited(string $tool, ProcessResult $result): Failure
    {
        return new Failure(Failure::FAILED, sprintf('%s failed', $tool), sprintf('exit code %d, output: %s', $result->exitCode, trim($result->stderr . "\n" . $result->stdout)));
    }

    public static function unreadable(string $tool, \JsonException $e): Failure
    {
        return new Failure(Failure::FAILED, sprintf('%s printed no JSON report', $tool), $e->getMessage(), $e);
    }
}
