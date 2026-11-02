<?php

declare(strict_types=1);

namespace Drupdater\Agent\Site;

use Drupdater\Agent\Failure;

/**
 * A site's exported core.extension.yml, edited by line so everything drupdater does not change
 * stays as the project wrote it. Drupal exports it in one shape: a `module:` mapping of names to
 * weights, a `theme:` one, and `profile:`.
 */
final class CoreExtension
{
    /**
     * @param list<string> $lines without their line breaks
     */
    private function __construct(private readonly array $lines, private readonly int $module)
    {
    }

    /** @throws Failure when the file cannot be read or has no module section */
    public static function read(string $path): self
    {
        $content = is_file($path) ? @file_get_contents($path) : false;
        if ($content === false) {
            throw new Failure(Failure::FAILED, sprintf('failed to read core extension file %s', $path));
        }
        $lines = explode("\n", rtrim($content, "\n"));
        $module = array_search('module:', array_map('rtrim', $lines), true);
        if ($module === false) {
            throw new Failure(Failure::FAILED, sprintf('core extension file %s has no module section', $path));
        }
        return new self($lines, $module);
    }

    public function sqliteEnabled(): bool
    {
        foreach ($this->moduleLines() as $line) {
            if (preg_match('/^\s+sqlite:\s*0\s*$/', $line) === 1) {
                return true;
            }
        }
        return false;
    }

    /** The sqlite module enabled at weight 0, which Drupal needs to install on SQLite at all. */
    public function withSqlite(): self
    {
        if ($this->sqliteEnabled()) {
            return $this;
        }
        $entries = $this->moduleLines();
        $indent = $entries === [] ? '  ' : (string) preg_replace('/\S.*$/', '', $entries[0]);
        $after = $this->module + count($entries) + 1;
        $lines = [...array_slice($this->lines, 0, $after), $indent . 'sqlite: 0', ...array_slice($this->lines, $after)];
        return new self($lines, $this->module);
    }

    /**
     * Without the profile, as `profile:` and as a module: site:install --existing-config refuses a
     * configuration whose profile it would install as well.
     */
    public function withoutProfile(string $profile): self
    {
        $lines = [];
        $module = 0;
        foreach ($this->lines as $i => $line) {
            $trimmed = trim($line);
            if ($trimmed === 'profile: ' . $profile || str_starts_with($trimmed, $profile . ':')) {
                continue;
            }
            if ($i === $this->module) {
                $module = count($lines);
            }
            $lines[] = $line;
        }
        return new self($lines, $module);
    }

    /** @throws Failure when the file cannot be written */
    public function write(string $path): void
    {
        if (@file_put_contents($path, implode("\n", $this->lines) . "\n") === false) {
            throw new Failure(Failure::FAILED, sprintf('failed to write core extension file %s', $path));
        }
    }

    /** @return list<string> the module section's entries, the lines indented under `module:` */
    private function moduleLines(): array
    {
        $entries = [];
        foreach (array_slice($this->lines, $this->module + 1) as $line) {
            if ($line === '' || !ctype_space($line[0])) {
                break;
            }
            $entries[] = $line;
        }
        return $entries;
    }
}
