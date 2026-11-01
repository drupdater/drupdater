<?php

declare(strict_types=1);

namespace Drupdater\Agent\Command;

use Composer\Factory;
use Composer\Config\JsonConfigSource;
use Composer\Json\JsonFile;
use Composer\Package\Locker;
use Drupdater\Agent\Failure;

/**
 * Writes extra.patches into composer.json and the lock's content-hash to match, which hashes
 * extra. Nothing is resolved: the lock is otherwise left byte for byte as it was.
 *
 * Request: {patches}, an object, written as given. Result: {}.
 */
final class WritePatchesCommand extends AgentCommand
{
    private const CONTENT_HASH = '{("content-hash"\s*:\s*)"[^"]*"}';

    protected function configure(): void
    {
        $this->setName('drupdater:write-patches')
            ->setDescription('Writes extra.patches and updates the lock\'s content-hash to match.')
            ->setHidden(true);
    }

    protected function handle(array $request): array
    {
        $patches = $this->rawMember('patches');
        if (!$patches instanceof \stdClass) {
            throw new Failure(Failure::INVALID_REQUEST, 'patches must be an object');
        }

        $composerFile = Factory::getComposerFile();
        $lock = new JsonFile(Factory::getLockFile($composerFile));
        if (!$lock->exists()) {
            throw new Failure(Failure::FAILED, sprintf('the project has no %s', basename($lock->getPath())));
        }

        $lockContents = (string) file_get_contents($lock->getPath());
        if (preg_match(self::CONTENT_HASH, $lockContents) !== 1) {
            throw new Failure(Failure::FAILED, sprintf('%s has no content-hash', basename($lock->getPath())));
        }

        self::writePatches($composerFile, $patches);

        $hash = Locker::getContentHash((string) file_get_contents($composerFile));
        // In the text: decoding and re-encoding the lock would turn its {} into [].
        file_put_contents($lock->getPath(), preg_replace(self::CONTENT_HASH, '${1}"' . $hash . '"', $lockContents, 1));
        return [];
    }

    /**
     * Composer's own surgical edit, which keeps the file's formatting, unless it came out wrong:
     * JsonManipulator turns an object keyed "0", "1"… into a list. Then the file is re-encoded.
     */
    private static function writePatches(string $composerFile, \stdClass $patches): void
    {
        $file = new JsonFile($composerFile);
        (new JsonConfigSource($file))->addProperty('extra.patches', $patches);

        $manifest = json_decode((string) file_get_contents($composerFile), false, 512, JSON_THROW_ON_ERROR);
        if (JsonFile::encode($manifest->extra->patches ?? null) === JsonFile::encode($patches)) {
            return;
        }
        $manifest->extra->patches = $patches;
        file_put_contents($composerFile, JsonFile::encode($manifest) . "\n");
    }
}
