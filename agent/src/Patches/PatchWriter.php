<?php

declare(strict_types=1);

namespace Drupdater\Agent\Patches;

use Composer\Config\JsonConfigSource;
use Composer\Json\JsonFile;
use Composer\Package\Locker;
use Drupdater\Agent\Failure;

/**
 * Writes extra.patches into composer.json and the lock's content-hash to match, which hashes
 * extra. Nothing is resolved: the lock is otherwise left byte for byte as it was.
 */
class PatchWriter
{
    private const CONTENT_HASH = '{("content-hash"\s*:\s*)"[^"]*"}';

    public function __construct(private readonly string $composerFile, private readonly string $lockFile)
    {
    }

    /**
     * @param \stdClass $patches written as given, empty objects and key order included
     *
     * @throws Failure when there is no lock, or it has no content-hash; nothing is written then
     */
    public function write(\stdClass $patches): void
    {
        $lockContents = @file_get_contents($this->lockFile);
        if ($lockContents === false) {
            throw new Failure(Failure::FAILED, sprintf('the project has no %s', basename($this->lockFile)));
        }
        if (preg_match(self::CONTENT_HASH, $lockContents) !== 1) {
            throw new Failure(Failure::FAILED, sprintf('%s has no content-hash', basename($this->lockFile)));
        }

        $this->writePatches($patches);

        $hash = Locker::getContentHash((string) file_get_contents($this->composerFile));
        // In the text: decoding and re-encoding the lock would turn its {} into [].
        file_put_contents($this->lockFile, preg_replace(self::CONTENT_HASH, '${1}"' . $hash . '"', $lockContents, 1));
    }

    /**
     * Composer's own surgical edit, which keeps the file's formatting, unless it came out wrong:
     * JsonManipulator turns an object keyed "0", "1"… into a list. Then the file is re-encoded.
     */
    private function writePatches(\stdClass $patches): void
    {
        (new JsonConfigSource(new JsonFile($this->composerFile)))->addProperty('extra.patches', $patches);

        $manifest = json_decode((string) file_get_contents($this->composerFile), false, 512, JSON_THROW_ON_ERROR);
        if (JsonFile::encode($manifest->extra->patches ?? null) === JsonFile::encode($patches)) {
            return;
        }
        $manifest->extra->patches = $patches;
        file_put_contents($this->composerFile, JsonFile::encode($manifest) . "\n");
    }
}
