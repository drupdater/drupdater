<?php

declare(strict_types=1);

namespace Drupdater\Agent\Addon;

use Drupdater\Agent\Log;
use Drupdater\Agent\Project\Project;
use Drupdater\Agent\Tools\Rector;

/**
 * deprecations_remover: rewrites deprecated API calls in the custom code with Drupal Rector.
 */
final class DeprecationsRemover implements Addon
{
    public const COMMIT_MESSAGE = 'Remove deprecations';

    /**
     * @param \Closure(): Project $project
     * @param \Closure(): Rector $projectRector the project's own, for a project with drupal-rector
     * @param \Closure(): Rector $bundledRector drupdater's, for a project with no Rector at all
     */
    public function __construct(
        private readonly Log $log,
        private readonly \Closure $project,
        private readonly \Closure $projectRector,
        private readonly \Closure $bundledRector,
    ) {
    }

    public static function name(): string
    {
        return 'deprecations_remover';
    }

    public static function hooks(): array
    {
        return [EventName::PostCodeUpdate->value => Stage::Change];
    }

    public static function create(Services $services): static
    {
        return new self($services->log, $services->project(...), $services->projectRector(...), $services->bundledRector(...));
    }

    public function handle(Event $event): HookResult
    {
        $this->log->info('removing deprecations');

        $project = ($this->project)();
        $rector = $this->rectorFor($project);
        if ($rector === null) {
            return new HookResult();
        }

        $result = $rector->process($project->customCodeDirectories);
        if ($result['changed_files'] === []) {
            $this->log->debug('no deprecations to remove');
            return new HookResult();
        }
        return new HookResult(self::fixes($result), [new Commit(self::COMMIT_MESSAGE, $result['changed_files'])]);
    }

    public static function report(Findings $findings): mixed
    {
        $fixes = $findings->shared(EventName::PostCodeUpdate)?->finding();
        return is_array($fixes) && $fixes !== [] ? $fixes : null;
    }

    /** The Rector that can load the project's code, or null when neither can. */
    private function rectorFor(Project $project): ?Rector
    {
        if ($project->installed('palantirnet/drupal-rector')) {
            return ($this->projectRector)();
        }
        // Ours would load the project's autoloader, and with it a second Rector's classes.
        if ($project->installed('rector/rector')) {
            $this->log->warning('skipping deprecation removal: the project requires rector/rector but not palantirnet/drupal-rector');
            return null;
        }
        return ($this->bundledRector)();
    }

    /**
     * Which rules fired on which file, which name the deprecation removed where "the file changed"
     * does not; both sorted, so two runs over unchanged code report byte-identically.
     *
     * @param array{changed_files: list<string>, file_diffs: list<array{file: string, applied_rectors: list<string>}>} $result
     * @return list<array{file: string, applied_rectors?: list<string>}>
     */
    public static function fixes(array $result): array
    {
        $rules = [];
        foreach ($result['file_diffs'] as $diff) {
            $rules[$diff['file']] = $diff['applied_rectors'];
        }
        $fixes = [];
        foreach ($result['changed_files'] as $file) {
            $applied = $rules[$file] ?? [];
            sort($applied, SORT_STRING);
            $fixes[] = $applied === [] ? ['file' => $file] : ['file' => $file, 'applied_rectors' => $applied];
        }
        usort($fixes, static fn (array $a, array $b): int => strcmp($a['file'], $b['file']));
        return $fixes;
    }
}
