<?php

declare(strict_types=1);

namespace Drupdater\Agent\PatchCheck;

use Composer\Composer;
use Composer\Downloader\TransportException;
use Composer\Package\AliasPackage;
use Composer\Package\BasePackage;
use Composer\Package\Package;
use Composer\Util\SyncHelper;
use Drupdater\Agent\Failure;

/**
 * Fetches a package through the project's own Composer: its repositories, in their order, and
 * its credentials. Nothing is installed into the project; no installer event fires.
 */
final class ComposerPackageFetcher implements PackageFetcher
{
    public const NOT_AVAILABLE = 'not available from any configured repository';
    public const AUTH_FAILED = 'repository authentication failed';
    public const DOWNLOAD_FAILED = 'a required download failed';

    public function __construct(private readonly Composer $composer)
    {
    }

    public function fetch(string $name, string $version, string $reference, string $targetDir): void
    {
        try {
            $package = $this->composer->getRepositoryManager()->findPackage($name, $version);
            if ($package === null) {
                throw new Failure(Failure::UNOBTAINABLE, self::NOT_AVAILABLE, sprintf('%s %s', $name, $version));
            }
            SyncHelper::downloadAndInstallPackageSync(
                $this->composer->getLoop(),
                $this->composer->getDownloadManager(),
                $targetDir,
                self::atReference($package, $reference),
            );
        }
        catch (Failure $failure) {
            throw $failure;
        }
        catch (\Exception $e) {
            throw self::unobtainable($e);
        }
    }

    public static function unobtainable(\Exception $e): Failure
    {
        $reason = $e instanceof TransportException && in_array($e->getStatusCode(), [401, 403], true)
            ? self::AUTH_FAILED
            : self::DOWNLOAD_FAILED;
        return new Failure(Failure::UNOBTAINABLE, $reason, $e->getMessage(), $e);
    }

    // A lock entry like "dev-1.x abc123" names a commit; the repository only knows the branch head.
    private static function atReference(BasePackage $package, string $reference): BasePackage
    {
        if ($package instanceof AliasPackage) {
            $package = $package->getAliasOf();
        }
        if ($reference === '' || !$package instanceof Package) {
            return $package;
        }
        $package = clone $package;
        $package->setSourceDistReferences($reference);
        return $package;
    }
}
