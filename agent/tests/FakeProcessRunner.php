<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests;

use Drupdater\Agent\Process\ProcessResult;
use Drupdater\Agent\Process\ProcessRunner;

/**
 * Answers each run with the next scripted result, and records what was run, where and with
 * which environment.
 */
final class FakeProcessRunner extends ProcessRunner
{
    /** @var list<array{command: list<string>, cwd: string, env: array<string, string>}> */
    public array $runs = [];

    /**
     * @param list<ProcessResult|\Closure(list<string>): ProcessResult> $results
     */
    public function __construct(private array $results = [])
    {
    }

    public function run(array $command, string $cwd, array $env = []): ProcessResult
    {
        $this->runs[] = ['command' => $command, 'cwd' => $cwd, 'env' => $env];
        $result = array_shift($this->results) ?? throw new \LogicException('unexpected run of ' . implode(' ', $command));
        return $result instanceof \Closure ? $result($command) : $result;
    }
}
