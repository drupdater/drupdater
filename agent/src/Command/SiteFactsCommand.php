<?php

declare(strict_types=1);

namespace Drupdater\Agent\Command;

use Drupdater\Agent\Failure;

/**
 * Reads what drupdater and the addons read from a site, in one bootstrap; see drupdater:facts in
 * resources/drush.
 *
 * Request: {site, update_status}: update_status asks drupal.org which modules are unsupported.
 * Result: {facts}, as drush printed them.
 */
final class SiteFactsCommand extends SiteCommand
{
    protected function configure(): void
    {
        $this->setName('drupdater:site-facts')
            ->setDescription('Reads a site\'s facts: its paths, modules and pending update hooks.')
            ->setHidden(true);
    }

    protected function handleSite(string $site, array $request): array
    {
        $updateStatus = $request['update_status'] ?? null;
        if (!is_bool($updateStatus)) {
            throw new Failure(Failure::INVALID_REQUEST, 'update_status must be a boolean');
        }
        return ['facts' => $this->drush()->facts($site, $updateStatus)];
    }
}
