<?php

declare(strict_types=1);

namespace Drupdater\Agent\MergeRequest;

/**
 * One section of the description, rendered from the report's addon sections.
 */
interface Section
{
    /** Names the section's golden file and its place in the published examples. */
    public function name(): string;

    /**
     * @param \stdClass $addons the report's addons object, by addon name
     * @param bool $links false once the description is over the size limit with them
     * @return string|null the markdown, or null when the report has nothing for this section
     */
    public function render(\stdClass $addons, bool $links): ?string;
}
