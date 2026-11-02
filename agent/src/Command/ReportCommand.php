<?php

declare(strict_types=1);

namespace Drupdater\Agent\Command;

use Drupdater\Agent\Addon\Catalogue;
use Drupdater\Agent\Addon\Findings;
use Drupdater\Agent\Failure;

/**
 * Builds the addons' sections of the run report from what their hooks found. Needs no project:
 * a section is a function of its addon's findings alone.
 *
 * Request: {addons[{addon, findings[{event, site, finding, committed[]}]}]}. Result: {sections},
 * by addon name, leaving out the addons with nothing to report.
 */
final class ReportCommand extends AgentCommand
{
    private readonly Catalogue $catalogue;

    public function __construct(?Catalogue $catalogue = null)
    {
        $this->catalogue = $catalogue ?? Catalogue::builtin();
        parent::__construct();
    }

    protected function configure(): void
    {
        $this->setName('drupdater:report')
            ->setDescription('Builds the addons\' sections of the run report.')
            ->setHidden(true);
    }

    protected function handle(array $request): array
    {
        $addons = $this->rawMember('addons');
        if (!is_array($addons) || !array_is_list($addons)) {
            throw new Failure(Failure::INVALID_REQUEST, 'addons must be a list');
        }
        $sections = [];
        $seen = [];
        foreach ($addons as $addon) {
            $name = $addon instanceof \stdClass ? ($addon->addon ?? null) : null;
            $entry = is_string($name) ? $this->catalogue->find($name) : null;
            if ($entry === null) {
                throw new Failure(Failure::INVALID_REQUEST, sprintf('no addon %s is in the catalogue', json_encode($name)));
            }
            if (isset($seen[$entry->name()])) {
                throw new Failure(Failure::INVALID_REQUEST, sprintf('%s is listed twice', $entry->name()));
            }
            $seen[$entry->name()] = true;
            $section = $entry->addon::report(Findings::fromJson($addon->findings ?? null));
            if ($section !== null) {
                $sections[$entry->name()] = $section;
            }
        }
        return ['sections' => (object) $sections];
    }
}
