<?php

declare(strict_types=1);

namespace Drupdater\App\Agent;

use Drupdater\App\Failure;
use Drupdater\App\Json;
use Drupdater\App\Log;
use Drupdater\App\Protocol;
use Symfony\Component\Process\Process;

/**
 * Calls the agent: one `composer drupdater:*` process per call, so a call always runs with the
 * project's autoloader as it stands now, never one an earlier call loaded.
 */
final class Agent
{
    /**
     * @param non-empty-list<string> $composer how to run Composer: its argv before the command
     */
    public function __construct(private readonly Log $log, private readonly array $composer = ['composer'])
    {
    }

    /**
     * Runs command in dir with request and returns its result. The agent's log entries are added
     * to this agent's log, whatever the outcome.
     *
     * @param array<string, mixed> $request
     * @throws AgentError
     */
    public function call(string $dir, string $command, array $request): \stdClass
    {
        $process = new Process([...$this->composer, $command, '--no-interaction', '--no-ansi'], $dir, null, Protocol::encode(['protocol' => Protocol::VERSION, 'request' => (object) $request]), null);
        $process->run();
        $stderr = trim($process->getErrorOutput());
        $this->log->debug($command, ['dir' => $dir, 'stderr' => $stderr]);

        $reply = $this->lastEnvelope($process->getOutput(), $command);
        if ($reply === null) {
            throw new AgentError($command, Failure::INTERNAL, 'no reply envelope on stdout', sprintf('exit code %s', Json::string($process->getExitCode(), 'none')), $stderr);
        }
        foreach (Json::maps(Json::at($reply, 'log')) as $entry) {
            $this->log->add(Json::string($entry['level'] ?? null), Json::string($entry['message'] ?? null), Json::map($entry['context'] ?? null));
        }
        $version = Json::at($reply, 'protocol');
        if ($version !== Protocol::VERSION) {
            throw new AgentError($command, Failure::PROTOCOL_MISMATCH, sprintf('agent speaks protocol %s, the app speaks %d', Protocol::encode($version), Protocol::VERSION));
        }
        if (Json::at($reply, 'ok') !== true) {
            throw new AgentError(
                $command,
                Json::string(Json::at($reply, 'error', 'kind'), Failure::INTERNAL),
                Json::string(Json::at($reply, 'error', 'reason')),
                Json::string(Json::at($reply, 'error', 'message')),
                $stderr,
            );
        }
        if (!$process->isSuccessful()) {
            throw new AgentError($command, Failure::INTERNAL, 'answered but failed', sprintf('exit code %s', Json::string($process->getExitCode())), $stderr);
        }
        $result = Json::at($reply, 'result');
        if (!$result instanceof \stdClass) {
            throw new AgentError($command, Failure::INTERNAL, 'the result is not an object');
        }
        return $result;
    }

    /**
     * The last line of stdout: another plugin may print before the agent answers, and the agent
     * always answers last.
     */
    private function lastEnvelope(string $stdout, string $command): ?\stdClass
    {
        $lines = explode("\n", trim($stdout));
        $reply = json_decode(end($lines), false);
        return $reply instanceof \stdClass && Json::at($reply, 'command') === $command ? $reply : null;
    }
}
