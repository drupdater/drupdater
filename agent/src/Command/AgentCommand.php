<?php

declare(strict_types=1);

namespace Drupdater\Agent\Command;

use Composer\Command\BaseCommand;
use Drupdater\Agent\Failure;
use Drupdater\Agent\Protocol;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Input\StreamableInputInterface;
use Symfony\Component\Console\Output\OutputInterface;

/**
 * A command that answers one JSON request with one envelope, whatever goes wrong.
 */
abstract class AgentCommand extends BaseCommand
{
    /** Where a command that runs another of Composer's sends that one's output. */
    protected ?OutputInterface $output = null;

    private string $body = '';

    /**
     * @param array<string, mixed> $request
     * @return array<string, mixed>
     */
    abstract protected function handle(array $request): array;

    protected function execute(InputInterface $input, OutputInterface $output): int
    {
        $name = (string) $this->getName();
        $this->output = $output;
        try {
            $this->body = $this->readRequest($input);
            $result = $this->handle(Protocol::decodeRequest($this->body));
            $output->writeln(Protocol::success($name, $result), OutputInterface::OUTPUT_RAW);
            return 0;
        }
        catch (Failure $failure) {
            $output->writeln(Protocol::failure($name, $failure), OutputInterface::OUTPUT_RAW);
            return 1;
        }
        catch (\Throwable $e) {
            $failure = new Failure(Failure::INTERNAL, 'the agent failed', $e->getMessage(), $e);
            $output->writeln(Protocol::failure($name, $failure), OutputInterface::OUTPUT_RAW);
            return 1;
        }
    }

    /**
     * The request's member as written, objects as objects: decoded into arrays, {} would come
     * back as [] and a key such as "0" would turn an object into a list.
     */
    protected function rawMember(string $key): mixed
    {
        $decoded = json_decode($this->body, false, 512, JSON_THROW_ON_ERROR);
        return $decoded->request->{$key} ?? null;
    }

    private function readRequest(InputInterface $input): string
    {
        $stream = $input instanceof StreamableInputInterface ? $input->getStream() : null;
        $body = stream_get_contents($stream ?? STDIN);
        return $body === false ? '' : $body;
    }
}
