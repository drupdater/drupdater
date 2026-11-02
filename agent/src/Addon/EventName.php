<?php

declare(strict_types=1);

namespace Drupdater\Agent\Addon;

/**
 * The points in a run addons hook into, in the order a run reaches them.
 */
enum EventName: string
{
    case PreComposerUpdate = 'pre-composer-update';
    case PostComposerUpdate = 'post-composer-update';
    case PostCodeUpdate = 'post-code-update';
    case PreSiteUpdate = 'pre-site-update';
    case PostSiteUpdate = 'post-site-update';

    public function perSite(): bool
    {
        return $this === self::PreSiteUpdate || $this === self::PostSiteUpdate;
    }
}
