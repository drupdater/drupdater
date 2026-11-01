<?php

declare(strict_types=1);

namespace Drupdater\Agent\Command;

use Composer\Util\ProcessExecutor;
use Drupdater\Agent\Failure;
use Drupdater\Agent\PatchCheck\ComposerPackageFetcher;
use Drupdater\Agent\PatchCheck\PatchApplier;
use Drupdater\Agent\PatchCheck\PatchChecker;
use Drupdater\Agent\PatchCheck\PatchFiles;

/**
 * Reports whether patches still apply, in order, to a package at a version.
 *
 * Request: {package, version, reference, patches[]}. Result: {outcome: "applies"} or
 * {outcome: "rejected", patch}. A package or patch that cannot be fetched is an "unobtainable"
 * failure, never a rejection.
 */
final class PatchCheckCommand extends AgentCommand
{
    /**
     * @param (\Closure(): PatchChecker)|null $checker
     */
    public function __construct(private readonly ?\Closure $checker = null)
    {
        parent::__construct();
    }

    protected function configure(): void
    {
        $this->setName('drupdater:patch-check')
            ->setDescription('Reports whether patches still apply to a package at a version.')
            ->setHidden(true);
    }

    protected function handle(array $request): array
    {
        $name = self::string($request, 'package');
        $version = self::string($request, 'version');
        $reference = isset($request['reference']) ? self::string($request, 'reference') : '';
        $patches = $request['patches'] ?? null;
        if (!is_array($patches) || $patches === [] || !array_is_list($patches) || array_filter($patches, 'is_string') !== $patches) {
            throw new Failure(Failure::INVALID_REQUEST, 'patches must be a non-empty list of strings');
        }

        return $this->checker()->check($name, $version, $reference, $patches);
    }

    private function checker(): PatchChecker
    {
        if ($this->checker !== null) {
            return ($this->checker)();
        }
        $composer = $this->requireComposer();
        return new PatchChecker(
            new ComposerPackageFetcher($composer),
            new PatchFiles($composer->getLoop()->getHttpDownloader()),
            new PatchApplier(new ProcessExecutor($this->getIO())),
            $composer->getPackage()->getExtra()['patchLevel'] ?? [],
        );
    }

    /**
     * @param array<string, mixed> $request
     */
    private static function string(array $request, string $key): string
    {
        $value = $request[$key] ?? null;
        if (!is_string($value) || ($value === '' && $key !== 'reference')) {
            throw new Failure(Failure::INVALID_REQUEST, sprintf('%s must be a non-empty string', $key));
        }
        return $value;
    }
}
