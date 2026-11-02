<?php

declare(strict_types=1);

namespace Drupdater\Agent\Addon;

use Drupdater\Agent\Audit\LockAudit;
use Drupdater\Agent\Json;
use Drupdater\Agent\Log;

/**
 * composer_audit: audits the lock before and after the update, so a routine update that closes
 * an advisory says so. Only a security run lets the audit decide the update's scope, and ends the
 * run when there is nothing to fix.
 *
 * @phpstan-import-type Advisory from LockAudit
 * @phpstan-type Abandoned array{packageName: string, replacement: string}
 */
final class ComposerAudit implements Addon
{
    /**
     * @param \Closure(): array{advisories: list<Advisory>, abandoned: list<array{package: string, replacement: string|null}>} $audit
     *   LockAudit's audit of the lock as it stands when called
     */
    public function __construct(private readonly Log $log, private readonly \Closure $audit)
    {
    }

    public static function name(): string
    {
        return 'composer_audit';
    }

    /** Prepare: the stages after it settle the update a security run restricts. */
    public static function hooks(): array
    {
        return [
            EventName::PreComposerUpdate->value => Stage::Prepare,
            EventName::PostCodeUpdate->value => Stage::Inspect,
        ];
    }

    public static function create(Services $services): static
    {
        return new self($services->log, static fn (): array => $services->audit()->toArray());
    }

    public function handle(Event $event): HookResult
    {
        $audit = self::reportShape(($this->audit)());
        if ($event->name === EventName::PreComposerUpdate) {
            return $this->before($event, $audit);
        }

        $before = Json::maps(Json::at($event->earlier->shared(EventName::PreComposerUpdate)?->finding(), 'advisories'));
        $this->log->info('security advisories', [
            'fixed' => count(self::fixed($before, $audit['advisories'])),
            'unresolved' => count($audit['advisories']),
            'abandoned' => count(self::abandoned($audit['abandoned'])),
        ]);
        return new HookResult($audit);
    }

    /**
     * The advisories it fixed, the ones it did not, and the non-Drupal packages left abandoned.
     * Null without the audit after the update: every advisory found before would read as fixed.
     */
    public static function report(Findings $findings): mixed
    {
        $after = $findings->shared(EventName::PostCodeUpdate)?->finding();
        if (!is_array($after)) {
            return null;
        }
        $remaining = Json::maps(Json::at($after, 'advisories'));
        $fixed = self::fixed(Json::maps(Json::at($findings->shared(EventName::PreComposerUpdate)?->finding(), 'advisories')), $remaining);
        $abandoned = self::abandoned(array_map(
            static fn (array $package): array => ['packageName' => Json::string(Json::at($package, 'packageName')), 'replacement' => Json::string(Json::at($package, 'replacement'))],
            Json::maps(Json::at($after, 'abandoned')),
        ));
        if ($fixed === [] && $remaining === [] && $abandoned === []) {
            return null;
        }
        return ['fixed' => $fixed, 'remaining' => $remaining, 'abandoned' => $abandoned];
    }

    /**
     * The advisories present before but not after, in their order before.
     *
     * @param list<array<string, mixed>> $before
     * @param list<array<string, mixed>> $after
     * @return list<array<string, mixed>>
     */
    public static function fixed(array $before, array $after): array
    {
        $open = array_flip(array_map(self::key(...), $after));
        return array_values(array_filter($before, static fn (array $advisory): bool => !isset($open[self::key($advisory)])));
    }

    /**
     * Identifies an advisory without collapsing the CVE-less ones onto one key: a collision would
     * report an advisory fixed while it is still open. Package and title are JSON-quoted rather
     * than joined on a separator, because a title is free text.
     *
     * @param array<string, mixed> $advisory
     */
    public static function key(array $advisory): string
    {
        $cve = Json::string(Json::at($advisory, 'cve'));
        if ($cve !== '') {
            return 'cve:' . $cve;
        }
        $id = Json::string(Json::at($advisory, 'advisoryId'));
        if ($id !== '') {
            return 'id:' . $id;
        }
        return 'pkg:' . json_encode([Json::string(Json::at($advisory, 'packageName')), Json::string(Json::at($advisory, 'title'))], JSON_THROW_ON_ERROR);
    }

    /**
     * Minus drupal/*: unsupported_modules reports those from drupal.org's release data, and
     * twice would read as two findings.
     *
     * @param list<Abandoned> $abandoned
     * @return list<Abandoned>
     */
    public static function abandoned(array $abandoned): array
    {
        return array_values(array_filter($abandoned, static fn (array $package): bool => !str_starts_with($package['packageName'], 'drupal/')));
    }

    /**
     * @param array{advisories: list<array<string, mixed>>, abandoned: list<Abandoned>} $audit
     */
    private function before(Event $event, array $audit): HookResult
    {
        if (!$event->security) {
            return new HookResult($audit);
        }
        $packages = array_values(array_unique(array_map(static fn (array $advisory): string => Json::string(Json::at($advisory, 'packageName')), $audit['advisories'])));
        if ($packages === []) {
            throw new AbortRun('No security advisories found');
        }
        if (in_array('drupal/core', $packages, true)) {
            $packages = [...$packages, 'drupal/core-recommended', 'drupal/core-composer-scaffold'];
        }
        return new HookResult($audit, plan: new Plan($packages, minimalChanges: true));
    }

    /**
     * LockAudit's audit in the report's own names, which the merge request reads; an absent
     * value as "".
     *
     * @param array{advisories: list<Advisory>, abandoned: list<array{package: string, replacement: string|null}>} $audit
     * @return array{advisories: list<array<string, mixed>>, abandoned: list<Abandoned>}
     */
    private static function reportShape(array $audit): array
    {
        return [
            'advisories' => array_map(static fn (array $advisory): array => [
                'reportedAt' => $advisory['reported_at'],
                'severity' => $advisory['severity'] ?? '',
                'advisoryId' => $advisory['advisory_id'],
                'cve' => $advisory['cve'] ?? '',
                'sources' => array_map(
                    static fn (array $source): array => ['name' => $source['name'], 'remoteId' => $source['remote_id']],
                    $advisory['sources'],
                ),
                'link' => $advisory['link'] ?? '',
                'packageName' => $advisory['package'],
                'affectedVersions' => $advisory['affected_versions'],
                'title' => $advisory['title'],
            ], $audit['advisories']),
            'abandoned' => array_map(
                static fn (array $package): array => ['packageName' => $package['package'], 'replacement' => $package['replacement'] ?? ''],
                $audit['abandoned'],
            ),
        ];
    }
}
