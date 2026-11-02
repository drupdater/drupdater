<?php

declare(strict_types=1);

namespace Drupdater\Agent\MergeRequest;

use Drupdater\Agent\Json;

/** The hooks each site ran, sites and hooks by name. One site needs no heading of its own. */
final class UpdateHooksSection implements Section
{
    private const NAME = 'update_hooks';

    public function name(): string
    {
        return self::NAME;
    }

    public function render(\stdClass $addons, bool $links): ?string
    {
        $sites = self::byName(Read::object($addons->update_hooks ?? null, self::NAME, 'the section'));
        if ($sites === []) {
            return null;
        }

        $out = "## 📄 Job Logs\n\n<details>\n<summary>⚙️ Update Hooks</summary>\n\n";
        foreach ($sites as $site => $hooks) {
            if (count($sites) > 1) {
                $out .= "### Site: {$site}\n\n";
            }
            $out .= "| Hook | Description |\n| ---- | ----------- |\n";
            foreach (self::byName(Read::object($hooks, self::NAME, 'site ' . $site)) as $hook => $details) {
                $details = Read::object($details, self::NAME, 'hook ' . $hook) ?? new \stdClass();
                $out .= sprintf("| %s | %s |\n", Markdown::cell((string) $hook), Markdown::cell(Read::string($details, 'description', self::NAME)));
            }
            $out .= "\n";
        }
        return $out . "</details>\n";
    }

    /**
     * @return array<string, mixed>
     */
    private static function byName(?\stdClass $object): array
    {
        $members = Json::map($object);
        ksort($members, SORT_STRING);
        return $members;
    }
}
