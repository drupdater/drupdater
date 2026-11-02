<?php

declare(strict_types=1);

namespace Drupdater\Agent\Command;

use Drupdater\Agent\Addon\Catalogue;
use Drupdater\Agent\Addon\CatalogueEntry;

/**
 * Lists the addons drupdater can run: which every run needs, which a normal run gets by default,
 * and the stage each hooks every event at. Needs no project.
 *
 * Request: {}. Result: {addons[{name, mandatory, default, hooks{event: stage}}]}, in catalogue order.
 */
final class AddonsCommand extends AgentCommand
{
    private readonly Catalogue $catalogue;

    public function __construct(?Catalogue $catalogue = null)
    {
        $this->catalogue = $catalogue ?? Catalogue::builtin();
        parent::__construct();
    }

    protected function configure(): void
    {
        $this->setName('drupdater:addons')
            ->setDescription('Lists the addons drupdater can run, and when each runs.')
            ->setHidden(true);
    }

    protected function handle(array $request): array
    {
        return ['addons' => array_map(static function (CatalogueEntry $entry): array {
            $described = $entry->toArray();
            $described['hooks'] = (object) $described['hooks'];
            return $described;
        }, $this->catalogue->entries())];
    }
}
