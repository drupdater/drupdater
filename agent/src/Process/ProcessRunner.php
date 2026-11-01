<?php

declare(strict_types=1);

namespace Drupdater\Agent\Process;

use Symfony\Component\Process\Process;

/**
 * Runs a tool to completion with its streams kept apart: a PHP notice on stderr must not corrupt
 * the JSON a tool prints on stdout. No timeout: drupdater bounds the whole run.
 */
class ProcessRunner
{
    /**
     * @param list<string> $command
     * @param array<string, string> $env added to the inherited environment, winning over it
     */
    public function run(array $command, string $cwd, array $env = []): ProcessResult
    {
        $process = new Process($command, $cwd, $env, null, null);
        $process->run();
        return new ProcessResult((int) $process->getExitCode(), $process->getOutput(), $process->getErrorOutput());
    }
}
