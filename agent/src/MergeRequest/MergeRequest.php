<?php

declare(strict_types=1);

namespace Drupdater\Agent\MergeRequest;

use Drupdater\Agent\Failure;

/**
 * A run's merge request, rendered from its report the way any consumer reads it, so the
 * description cannot say anything the report does not publish.
 */
final class MergeRequest
{
    /** GitHub's cap on a pull request body, the tighter of the two platforms. */
    public const MAX_DESCRIPTION_BYTES = 65536;

    private const INTRO = 'This automated merge request by [Drupdater](https://github.com/drupdater/drupdater) includes updates '
        . 'for your Drupal site. Please review the changes carefully to ensure compatibility and stability before merging.';

    /**
     * @param list<Section> $sections in the description's order
     */
    public function __construct(private readonly array $sections)
    {
    }

    /** What needs a decision first, logs last. */
    public static function builtin(): self
    {
        return new self([
            new AllowPluginsSection(),
            new PatchesSection(),
            new DependencySection(),
            new AuditSection(),
            new EndOfLifeSection(),
            new UpdateHooksSection(),
        ]);
    }

    /**
     * Named after the run's mode and its start, in the zone the report gives it.
     *
     * @throws Failure when the report has no readable start
     */
    public static function title(\stdClass $report): string
    {
        $started = $report->started_at ?? null;
        $at = is_string($started) ? \DateTimeImmutable::createFromFormat(\DateTimeInterface::RFC3339_EXTENDED, self::millis($started)) : false;
        if ($at === false) {
            throw new Failure(Failure::INVALID_REQUEST, 'the report\'s started_at must be an RFC 3339 time');
        }
        if (($report->mode ?? null) === 'security') {
            return $at->format('Y-m-d') . ': Drupal Security Updates';
        }
        return $at->format('F Y') . ': Drupal Maintenance Updates';
    }

    /**
     * Over MAX_DESCRIPTION_BYTES it drops the dependency links, the one part of the description
     * that grows by more than a row per package.
     *
     * @throws Failure when a section is unreadable
     */
    public function description(\stdClass $addons): string
    {
        $description = $this->render($addons, true);
        if (strlen($description) <= self::MAX_DESCRIPTION_BYTES) {
            return $description;
        }
        return $this->render($addons, false);
    }

    /**
     * Each section the description of $addons holds, by name, in order.
     *
     * @return array<string, string>
     */
    public function sections(\stdClass $addons, bool $links = true): array
    {
        $rendered = [];
        foreach ($this->sections as $section) {
            $out = $section->render($addons, $links);
            if ($out !== null) {
                // Exactly one newline at the end, so sections are a blank line apart.
                $rendered[$section->name()] = rtrim($out, "\n") . "\n";
            }
        }
        return $rendered;
    }

    private function render(\stdClass $addons, bool $links): string
    {
        return self::INTRO . "\n" . implode('', array_map(static fn (string $section): string => "\n" . $section, $this->sections($addons, $links)));
    }

    /** Go writes from none to nine fractional digits; RFC3339_EXTENDED takes exactly three. */
    private static function millis(string $time): string
    {
        return (string) preg_replace_callback(
            '/^(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d)(?:\.(\d+))?(Z|[+-]\d\d:\d\d)$/',
            static fn (array $m): string => $m[1] . '.' . substr(str_pad($m[2], 3, '0'), 0, 3) . ($m[3] === 'Z' ? '+00:00' : $m[3]),
            $time,
        );
    }
}
