<?php

declare(strict_types=1);

namespace Drupdater\Agent\Addon;

use Drupdater\Agent\DrupalOrg\DrupalCode;
use Drupdater\Agent\DrupalOrg\DrupalOrg;
use Drupdater\Agent\DrupalOrg\Issue;
use Drupdater\Agent\DrupalOrg\IssueNumber;
use Drupdater\Agent\Failure;
use Drupdater\Agent\Log;
use Drupdater\Agent\PatchCheck\PatchChecker;
use Drupdater\Agent\Patches\DeclaredPatches;
use Drupdater\Agent\Patches\PatchDownloader;
use Drupdater\Agent\Patches\PatchUpdates;
use Drupdater\Agent\Patches\PatchWriter;
use Drupdater\Agent\Project\Project;
use Drupdater\Agent\Resolve\Resolver;

/**
 * composer_patches: keeps the project's composer-patches 1.x patches right across the update.
 * It drops the ones no longer needed, replaces the ones superseded on drupal.org, and holds a
 * package back whose patch cannot be made to apply.
 */
final class ComposerPatches implements Addon
{
    public const COMMIT_MESSAGE = 'Update patches';

    /**
     * @param \Closure(): Project $project
     * @param ?DrupalCode $drupalCode null without DRUPALCODE_ACCESS_TOKEN: a fix then stays
     *   unconfirmed, and no newer patch is looked for
     */
    public function __construct(
        private readonly Log $log,
        private readonly \Closure $project,
        private readonly Resolver $resolver,
        private readonly PatchChecker $checker,
        private readonly PatchWriter $writer,
        private readonly PatchDownloader $downloader,
        private readonly DrupalOrg $drupalOrg,
        private readonly ?DrupalCode $drupalCode,
    ) {
    }

    public static function name(): string
    {
        return 'composer_patches';
    }

    /** Change: it rewrites extra.patches, against the update as composer_audit restricted it. */
    public static function hooks(): array
    {
        return [EventName::PreComposerUpdate->value => Stage::Change];
    }

    public static function create(Services $services): static
    {
        return new self(
            $services->log,
            $services->project(...),
            $services->resolver(),
            $services->patchChecker(),
            $services->patchWriter(),
            $services->patchDownloader(),
            $services->drupalOrg(),
            $services->drupalCode(),
        );
    }

    /**
     * Checks the patches against the update as earlier stages planned it, and proposes pinning
     * each package whose patch cannot be made to apply.
     */
    public function handle(Event $event): HookResult
    {
        // Read before the dry run: it leaves the files as they were, and Composer's own view of
        // the lock in this process as the dry run left it.
        $project = ($this->project)();
        $patches = DeclaredPatches::fromJson($project->patches);
        $plan = $event->plan();
        $operations = $this->resolver->resolve($plan->only, $plan->keep, $plan->minimalChanges, false);

        $updates = new PatchUpdates();
        $this->updatePatches($event->projectDir, $project, $operations, $patches, $updates);

        $proposal = new Plan(keep: array_map(static fn (array $conflict): Pin => new Pin($conflict['package'], $conflict['fixed_version']), $updates->conflicts));
        if (!$updates->changes()) {
            return new HookResult($updates->toArray(), plan: $proposal);
        }

        $this->log->info('patches changed', ['removed' => count($updates->removed), 'updated' => count($updates->updated), 'conflicts' => count($updates->conflicts)]);
        $this->writer->write($patches->toJson());
        $paths = $updates->changedPaths(['composer.json', 'composer.lock'], self::isRemote(...));
        return new HookResult($updates->toArray(), [new Commit(self::COMMIT_MESSAGE, $paths)], $proposal);
    }

    public static function report(Findings $findings): mixed
    {
        $finding = $findings->shared(EventName::PreComposerUpdate)?->finding();
        if (!is_array($finding) || ($finding['removed'] === [] && $finding['updated'] === [] && $finding['conflicts'] === [])) {
            return null;
        }
        return ['removed' => $finding['removed'], 'updated' => $finding['updated'], 'conflicts' => $finding['conflicts']];
    }

    /**
     * @param list<array{action: string, package: string, from: string, to: string}> $operations
     */
    private function updatePatches(string $projectDir, Project $project, array $operations, DeclaredPatches $patches, PatchUpdates $updates): void
    {
        $this->log->debug('processing composer patches', ['packages' => $patches->packages()]);
        $this->removeUninstalledPackagePatches($projectDir, $project, $patches, $updates);
        $this->removeDependencyProvidedPatches($project, $patches, $updates);

        foreach ($operations as $operation) {
            if ($operation['action'] === 'Upgrade' || $operation['action'] === 'Downgrade') {
                // A copy: processing renames and removes entries of the set it walks.
                foreach ($patches->of($operation['package']) as $patch) {
                    $this->processPatch($projectDir, $operation, $patch['description'], $patch['path'], $patches, $updates);
                }
                if (count($patches->of($operation['package'])) > 1) {
                    $this->checkCombined($projectDir, $operation, $patches, $updates);
                }
            }
            elseif ($operation['action'] === 'Remove') {
                $this->removePackagePatches($projectDir, $operation, $patches, $updates);
            }
        }
    }

    private function removeUninstalledPackagePatches(string $projectDir, Project $project, DeclaredPatches $patches, PatchUpdates $updates): void
    {
        foreach ($patches->packages() as $package) {
            if ($project->installed($package)) {
                continue;
            }
            foreach ($patches->of($package) as $patch) {
                $this->dropPatchFile($projectDir, $patch['path']);
                $this->log->info('removing patch: package no longer installed', ['package' => $package, 'patch' => $patch['path']]);
                $updates->remove($package, $patch['description'], $patch['path'], sprintf('%s is not installed in the project', $package));
            }
            $patches->removePackage($package);
        }
    }

    /**
     * Root patches a dependency already applies, which composer-patches would apply twice.
     * Remote ones only: a local path is relative to the package that declares it.
     */
    private function removeDependencyProvidedPatches(Project $project, DeclaredPatches $patches, PatchUpdates $updates): void
    {
        foreach ($patches->packages() as $package) {
            $provided = $project->dependencyPatches[$package] ?? [];
            foreach ($patches->of($package) as $patch) {
                if (!self::isRemote($patch['path']) || !in_array($patch['path'], $provided, true)) {
                    continue;
                }
                $this->log->info('removing patch: already applied by a dependency', ['package' => $package, 'patch' => $patch['path']]);
                $updates->remove($package, $patch['description'], $patch['path'], sprintf('Patch is already applied by a dependency of %s', $package));
                $patches->remove($package, $patch['description']);
            }
        }
    }

    /**
     * @param array{action: string, package: string, from: string, to: string} $operation
     */
    private function removePackagePatches(string $projectDir, array $operation, DeclaredPatches $patches, PatchUpdates $updates): void
    {
        foreach ($patches->of($operation['package']) as $patch) {
            $this->log->debug('removing patch', ['package' => $operation['package'], 'patch' => $patch['path']]);
            $updates->remove($operation['package'], $patch['description'], $patch['path'], sprintf('%s is no longer installed', $operation['package']));
            $this->dropPatchFile($projectDir, $patch['path']);
        }
        $patches->removePackage($operation['package']);
    }

    /**
     * @param array{action: string, package: string, from: string, to: string} $operation
     */
    private function processPatch(string $projectDir, array $operation, string $description, string $path, DeclaredPatches $patches, PatchUpdates $updates): void
    {
        $package = $operation['package'];
        $issueNumber = IssueNumber::find($description) ?? IssueNumber::find($path);

        $issue = null;
        if ($issueNumber !== null) {
            try {
                $issue = $this->drupalOrg->issue($issueNumber);
            }
            catch (\RuntimeException $e) {
                $this->log->error('failed to get issue', ['issue' => $issueNumber, 'error' => $e->getMessage()]);
                return;
            }
            $this->log->debug('fetched issue details', ['issue' => (array) $issue]);

            if ($this->drupalCode !== null && $issue->fixed() && $this->fixedUpstream($issue, $operation['to'])) {
                if (!$this->dropPatchFile($projectDir, $path)) {
                    // A patch whose file survived stays declared, rather than vanish unreported.
                    return;
                }
                $patches->remove($package, $description);
                $this->log->info('removing patch: issue fixed in new version', ['package' => $package, 'patch' => $path]);
                $updates->remove($package, $description, $path, sprintf('Issue [#%s](%s) is fixed in %s %s', $issue->id, $issue->url, $package, $operation['to']));
                return;
            }

            $canonical = sprintf('Issue #%s: [%s](%s)', $issue->id, $issue->title, $issue->url);
            if ($patches->rename($package, $description, $canonical)) {
                $description = $canonical;
            }
        }

        try {
            $applies = $this->applies($package, $operation['to'], [self::resolve($projectDir, $path)]);
        }
        catch (\Exception $e) {
            // An unverifiable patch is not a stale one: pinning here would hold the package back
            // on every run, and blame a conflict that never happened.
            $this->log->warning('could not check whether the patch still applies, leaving the package unpinned', ['package' => $package, 'patch' => $path, 'error' => self::describe($e)]);
            return;
        }
        if ($applies) {
            $this->log->debug('patch applies', ['package' => $package, 'version' => $operation['to'], 'patch' => $path]);
            return;
        }
        $this->log->debug('patch does not apply', ['package' => $package, 'version' => $operation['to'], 'patch' => $path]);

        if ($issue === null) {
            $this->log->info('patch does not apply, keeping current package version', ['package' => $package, 'version' => $operation['from'], 'patch' => $path]);
            $updates->conflict($package, $operation['from'], $operation['to'], $path, $description);
            return;
        }
        if ($this->drupalCode === null) {
            $this->log->info('patch does not apply and no drupalcode client is configured, keeping current package version', ['package' => $package, 'version' => $operation['from'], 'patch' => $path]);
            $updates->conflict($package, $operation['from'], $operation['to'], $path, $description);
            return;
        }
        $this->replaceWithMergeRequest($projectDir, $operation, $issue, $description, $path, $patches, $updates);
    }

    /**
     * Replaces a patch that no longer applies with the newest merge request from the issue's fork,
     * when that one does.
     *
     * @param array{action: string, package: string, from: string, to: string} $operation
     */
    private function replaceWithMergeRequest(string $projectDir, array $operation, Issue $issue, string $description, string $path, DeclaredPatches $patches, PatchUpdates $updates): void
    {
        $package = $operation['package'];
        try {
            $mergeRequests = $this->drupalCode?->issueForkMergeRequests($issue->project, $issue->id) ?? [];
        }
        catch (\RuntimeException $e) {
            $this->log->error('failed to find the issue fork\'s merge requests', ['error' => $e->getMessage()]);
            return;
        }
        if ($mergeRequests === []) {
            $this->log->debug('no merge requests found');
            return;
        }

        $mergeRequest = $mergeRequests[0];
        $newPath = sprintf('patches/%s/%s-%s-%s.diff', $issue->project, $issue->id, $mergeRequest->sha, self::fileNameOf($issue->title));
        $this->log->debug('downloading patch', ['url' => $mergeRequest->webUrl . '.diff', 'path' => dirname($newPath)]);
        try {
            $this->downloader->download($mergeRequest->webUrl . '.diff', $projectDir . '/' . $newPath);
        }
        catch (\RuntimeException $e) {
            $this->log->debug('failed to download patch', ['error' => $e->getMessage()]);
            return;
        }

        try {
            $applies = $this->applies($package, $operation['to'], [$projectDir . '/' . $newPath]);
        }
        catch (\Exception $e) {
            $this->log->warning('could not check whether the merge request patch applies, leaving the package unpinned', ['package' => $package, 'patch' => $newPath, 'error' => self::describe($e)]);
            @unlink($projectDir . '/' . $newPath);
            return;
        }
        if (!$applies) {
            $this->log->info('merge request does not apply, keeping current package version', ['package' => $package, 'version' => $operation['to'], 'patch' => $projectDir . '/' . dirname($newPath)]);
            @unlink($projectDir . '/' . $newPath);
            $updates->conflict($package, $operation['from'], $operation['to'], $path, $description);
            return;
        }
        if (!$this->dropPatchFile($projectDir, $path, 'failed to remove old patch file', quiet: true)) {
            @unlink($projectDir . '/' . $newPath);
            return;
        }
        $patches->setPath($package, $description, $newPath);
        $this->log->info('replacing patch', ['package' => $package, 'previous_patch' => $path, 'new_patch' => $newPath]);
        $updates->update($package, $description, $path, $newPath);
    }

    /**
     * @param array{action: string, package: string, from: string, to: string} $operation
     */
    private function checkCombined(string $projectDir, array $operation, DeclaredPatches $patches, PatchUpdates $updates): void
    {
        $paths = array_map(static fn (array $patch): string => self::resolve($projectDir, $patch['path']), $patches->of($operation['package']));
        try {
            $applies = $this->applies($operation['package'], $operation['to'], $paths);
        }
        catch (\Exception $e) {
            $this->log->warning('could not check whether the patches apply together, leaving the package unpinned', ['package' => $operation['package'], 'error' => self::describe($e)]);
            return;
        }
        if (!$applies) {
            $this->log->info('patches do not apply together, keeping current package version', ['package' => $operation['package'], 'version' => $operation['to']]);
            // No single patch to name: the set as a whole is what failed.
            $updates->conflict($operation['package'], $operation['from'], $operation['to'], '', 'Multiple patches do not apply together');
            return;
        }
        $this->log->debug('patches apply together', ['package' => $operation['package'], 'version' => $operation['to'], 'patch' => $paths]);
    }

    private function fixedUpstream(Issue $issue, string $version): bool
    {
        try {
            return $this->drupalCode?->isFixedIn($issue->project, $issue->id, $version) ?? false;
        }
        catch (\RuntimeException $e) {
            $this->log->error('failed to search commit history', ['error' => $e->getMessage()]);
            return false;
        }
    }

    /**
     * Whether the patches apply in order to the package at a version as the lock prints it.
     *
     * @param list<string> $patches
     *
     * @throws \Exception when that cannot be found out: the package or a patch could not be fetched
     */
    private function applies(string $package, string $version, array $patches): bool
    {
        // "1.x-dev 0424dff" names a commit of a branch.
        [$version, $reference] = array_pad(explode(' ', $version, 2), 2, '');
        $result = $this->checker->check($package, $version, $reference, $patches);
        if ($result['outcome'] === 'rejected') {
            $this->log->debug('patch rejected', ['package' => $package, 'patch' => $result['patch']]);
            return false;
        }
        return true;
    }

    /**
     * Removes a local patch's file; a remote patch has none.
     */
    private function dropPatchFile(string $projectDir, string $path, string $failure = 'failed to remove patch', bool $quiet = false): bool
    {
        if (self::isRemote($path) || @unlink($projectDir . '/' . $path)) {
            return true;
        }
        $quiet ? $this->log->debug($failure, ['patch' => $path]) : $this->log->error($failure, ['patch' => $path]);
        return false;
    }

    private static function describe(\Exception $e): string
    {
        return $e instanceof Failure ? trim($e->reason . ': ' . $e->getMessage(), ': ') : $e->getMessage();
    }

    public static function isRemote(string $path): bool
    {
        $scheme = parse_url($path, PHP_URL_SCHEME);
        return is_string($scheme) && in_array(strtolower($scheme), ['http', 'https'], true);
    }

    /** A patch-test project runs from a scratch directory, where a project-relative path points nowhere. */
    private static function resolve(string $projectDir, string $path): string
    {
        return self::isRemote($path) ? $path : $projectDir . '/' . $path;
    }

    /** An issue title as a file name component, which holds no path separator. */
    public static function fileNameOf(string $title): string
    {
        return (string) preg_replace('/[^a-zA-Z0-9\-_.]/', '', str_replace(' ', '_', strtolower($title)));
    }
}
