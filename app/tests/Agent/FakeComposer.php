<?php

declare(strict_types=1);

namespace Drupdater\App\Tests\Agent;

use Drupdater\App\Json;

/**
 * A composer that prints what the test tells it to and records what it was asked.
 */
final class FakeComposer
{
    private readonly string $spec;

    public function __construct(string $stdout, string $stderr = '', int $exit = 0)
    {
        $this->spec = (string) tempnam(sys_get_temp_dir(), 'drupdater-app-composer-');
        file_put_contents($this->spec, json_encode(['stdout' => $stdout, 'stderr' => $stderr, 'exit' => $exit]));
    }

    public function __destruct()
    {
        @unlink($this->spec);
        @unlink($this->spec . '.seen');
    }

    /**
     * @return non-empty-list<string>
     */
    public function argv(): array
    {
        return [PHP_BINARY, __DIR__ . '/../fixtures/composer.php', $this->spec];
    }

    /**
     * @return list<string>
     */
    public function seenArgv(): array
    {
        return Json::strings(Json::at($this->seen(), 'argv'));
    }

    public function seenStdin(): string
    {
        return Json::string(Json::at($this->seen(), 'stdin'));
    }

    public function seenCwd(): string
    {
        return Json::string(Json::at($this->seen(), 'cwd'));
    }

    private function seen(): mixed
    {
        return json_decode((string) file_get_contents($this->spec . '.seen'), false);
    }
}
