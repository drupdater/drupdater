<?php

declare(strict_types=1);

namespace Drupdater\App\Command;

use Drupdater\App\Failure;
use Drupdater\App\Log;
use Drupdater\App\Protocol;
use Symfony\Component\Console\Command\Command;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Input\StreamableInputInterface;
use Symfony\Component\Console\Output\OutputInterface;

/**
 * A command that answers one JSON request on stdin with one envelope on stdout, whatever goes
 * wrong, in the project in the working directory.
 */
abstract class EnvelopeCommand extends Command
{
    public function __construct(protected readonly Log $log)
    {
        parent::__construct();
    }

    /**
     * @return array<string, mixed>
     * @throws Failure
     */
    abstract protected function handle(string $dir, \stdClass $request): array;

    protected function execute(InputInterface $input, OutputInterface $output): int
    {
        $name = (string) $this->getName();
        try {
            $result = $this->handle((string) getcwd(), Protocol::decodeRequest($this->readRequest($input)));
            $output->writeln(Protocol::success($name, $result, $this->log->entries()), OutputInterface::OUTPUT_RAW);
            return self::SUCCESS;
        }
        catch (Failure $failure) {
            $output->writeln(Protocol::failure($name, $failure, $this->log->entries()), OutputInterface::OUTPUT_RAW);
            return self::FAILURE;
        }
        catch (\Throwable $e) {
            $failure = new Failure(Failure::INTERNAL, 'the app failed', $e->getMessage(), $e);
            $output->writeln(Protocol::failure($name, $failure, $this->log->entries()), OutputInterface::OUTPUT_RAW);
            return self::FAILURE;
        }
    }

    private function readRequest(InputInterface $input): string
    {
        $stream = $input instanceof StreamableInputInterface ? $input->getStream() : null;
        $body = stream_get_contents($stream ?? STDIN);
        return $body === false ? '' : $body;
    }
}
