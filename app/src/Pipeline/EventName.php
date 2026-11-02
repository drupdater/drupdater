<?php

declare(strict_types=1);

namespace Drupdater\App\Pipeline;

/**
 * The events addons hook into, in the order a run reaches them.
 */
enum EventName: string
{
    case PreComposerUpdate = 'pre-composer-update';
    case PostComposerUpdate = 'post-composer-update';
    case PostCodeUpdate = 'post-code-update';
    case PreSiteUpdate = 'pre-site-update';
    case PostSiteUpdate = 'post-site-update';

    public function order(): int
    {
        return (int) array_search($this, self::cases(), true);
    }
}
