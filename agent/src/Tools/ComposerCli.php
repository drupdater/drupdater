<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tools;

use Drupdater\Agent\Failure;
use Drupdater\Agent\Process\ProcessRunner;

/**
 * Composer run again as its own process, as a script's `@composer` is: for a plugin's command,
 * which needs a Composer that loaded the project afresh.
 */
class ComposerCli
{
    /**
     * @param string $composerBinary what Composer was started as, COMPOSER_BINARY
     */
    public function __construct(private readonly ProcessRunner $runner, private readonly string $composerBinary, private readonly string $projectDir)
    {
    }

    /**
     * ergebnis/composer-normalize's normalize, which also rewrites the lock's content-hash.
     *
     * @throws Failure when it fails
     */
    public function normalize(): void
    {
        $result = $this->runner->run([PHP_BINARY, $this->composerBinary, 'normalize', '--no-interaction'], $this->projectDir);
        if (!$result->succeeded()) {
            throw ToolFailure::exited('composer normalize', $result);
        }
    }
}
