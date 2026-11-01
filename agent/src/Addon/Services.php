<?php

declare(strict_types=1);

namespace Drupdater\Agent\Addon;

use Composer\Composer;
use Composer\Factory;
use Composer\IO\IOInterface;
use Composer\Policy\PolicyConfig;
use Composer\Util\Platform;
use Composer\Util\ProcessExecutor;
use Drupdater\Agent\Audit\LockAudit;
use Drupdater\Agent\DrupalOrg\DrupalCode;
use Drupdater\Agent\DrupalOrg\DrupalOrg;
use Drupdater\Agent\Failure;
use Drupdater\Agent\Log;
use Drupdater\Agent\PatchCheck\ComposerPackageFetcher;
use Drupdater\Agent\PatchCheck\PatchApplier;
use Drupdater\Agent\PatchCheck\PatchChecker;
use Drupdater\Agent\PatchCheck\PatchFiles;
use Drupdater\Agent\Patches\PatchDownloader;
use Drupdater\Agent\Patches\PatchWriter;
use Drupdater\Agent\Plugins\AllowPlugins;
use Drupdater\Agent\Process\ProcessRunner;
use Drupdater\Agent\Project\Project;
use Drupdater\Agent\Project\ProjectFacts;
use Drupdater\Agent\Resolve\Resolver;
use Drupdater\Agent\Tools\Bin;
use Drupdater\Agent\Tools\ComposerCli;
use Drupdater\Agent\Tools\Drush;
use Drupdater\Agent\Tools\Phpcs;
use Drupdater\Agent\Tools\Rector;

/**
 * The real tools an addon's create() builds it from, for the project of the running Composer.
 * Each is built when asked for, so an addon pays only for what it uses.
 */
final class Services
{
    /** Where the image installs tools/: the PHPCS and Rector a project without its own runs. */
    public const BUNDLED_TOOLS = '/opt/drupdater/tools/vendor/bin';

    /**
     * @param \Closure(): PolicyConfig $policy the project's audit policy, as Composer's own commands read it
     */
    public function __construct(
        private readonly Composer $composer,
        private readonly IOInterface $io,
        private readonly Resolver $resolver,
        private readonly \Closure $policy,
        public readonly Log $log,
        private readonly string $bundledTools = self::BUNDLED_TOOLS,
    ) {
    }

    public function composer(): Composer
    {
        return $this->composer;
    }

    public function projectDir(): string
    {
        return dirname($this->composerFile());
    }

    /** Read afresh on every call, so it reflects whatever rewrote composer.json or the lock. */
    public function project(): Project
    {
        return (new ProjectFacts($this->composer, $this->composerFile()))->project();
    }

    public function resolver(): Resolver
    {
        return $this->resolver;
    }

    public function allowPlugins(): AllowPlugins
    {
        return new AllowPlugins($this->composer, $this->composerFile());
    }

    public function audit(): LockAudit
    {
        return new LockAudit($this->composer, ($this->policy)());
    }

    public function patchChecker(): PatchChecker
    {
        return new PatchChecker(
            new ComposerPackageFetcher($this->composer),
            new PatchFiles($this->composer->getLoop()->getHttpDownloader()),
            new PatchApplier(new ProcessExecutor($this->io)),
            $this->composer->getPackage()->getExtra()['patchLevel'] ?? [],
        );
    }

    public function patchWriter(): PatchWriter
    {
        return new PatchWriter($this->composerFile(), Factory::getLockFile($this->composerFile()));
    }

    public function patchDownloader(): PatchDownloader
    {
        return new PatchDownloader($this->composer->getLoop()->getHttpDownloader());
    }

    public function drupalOrg(): DrupalOrg
    {
        return new DrupalOrg($this->composer->getLoop()->getHttpDownloader());
    }

    /** Null without DRUPALCODE_ACCESS_TOKEN: drupalcode answers no API call anonymously. */
    public function drupalCode(): ?DrupalCode
    {
        $token = Platform::getEnv(DrupalCode::TOKEN_VARIABLE);
        if (!is_string($token) || $token === '') {
            return null;
        }
        return new DrupalCode($this->composer->getLoop()->getHttpDownloader(), $token);
    }

    public function projectPhpcs(): Phpcs
    {
        return new Phpcs(new ProcessRunner(), $this->projectBin(), $this->projectDir());
    }

    public function bundledPhpcs(): Phpcs
    {
        return new Phpcs(new ProcessRunner(), new Bin($this->bundledTools), $this->projectDir());
    }

    public function projectRector(): Rector
    {
        return Rector::project(new ProcessRunner(), $this->projectBin(), self::rectorConfig(), $this->projectDir());
    }

    public function bundledRector(): Rector
    {
        return Rector::bundled(new ProcessRunner(), new Bin($this->bundledTools), self::rectorConfig(), $this->projectDir(), (string) $this->composer->getConfig()->get('vendor-dir'));
    }

    public function drush(): Drush
    {
        return new Drush(new ProcessRunner(), $this->projectBin(), $this->projectDir());
    }

    public function composerCli(): ComposerCli
    {
        $binary = Platform::getEnv('COMPOSER_BINARY');
        if (!is_string($binary) || $binary === '') {
            throw new Failure(Failure::INTERNAL, 'COMPOSER_BINARY is not set, so Composer cannot be run again');
        }
        return new ComposerCli(new ProcessRunner(), $binary, $this->projectDir());
    }

    /** drupdater's Rector config, shipped with the agent. */
    public static function rectorConfig(): string
    {
        return dirname(__DIR__, 2) . '/resources/rector.php';
    }

    private function projectBin(): Bin
    {
        return new Bin((string) $this->composer->getConfig()->get('bin-dir'));
    }

    private function composerFile(): string
    {
        $file = Factory::getComposerFile();
        return (string) (realpath($file) ?: $file);
    }
}
