<?php

declare(strict_types=1);

namespace Drupdater\Agent\MergeRequest;

final class AllowPluginsSection implements Section
{
    public function name(): string
    {
        return 'composer_allow_plugins';
    }

    public function render(\stdClass $addons, bool $links): ?string
    {
        $section = Read::object($addons->composer_allow_plugins ?? null, 'composer_allow_plugins', 'the section');
        $added = Read::strings($section->added ?? null, 'composer_allow_plugins', 'added');
        if ($added === []) {
            return null;
        }
        $list = implode('', array_map(static fn (string $plugin): string => "- {$plugin}\n", $added));
        $commands = implode('', array_map(static fn (string $plugin): string => "composer config allow-plugins.{$plugin} true\n", $added));
        return <<<MD
            ## 🔌 New Composer plugins

            During the update process, new Composer plugins were detected and added to the allowlist with their initial value set to false:

            {$list}
            Please review the changes and adjust the values as needed. Run the following command to allow the plugins:

            ```bash
            {$commands}```

            MD;
    }
}
