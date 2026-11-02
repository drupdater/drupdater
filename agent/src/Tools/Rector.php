<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tools;

use Drupdater\Agent\Failure;
use Drupdater\Agent\Process\ProcessRunner;

/**
 * Rector with drupdater's config, over the project's custom code.
 */
class Rector
{
    /**
     * @param list<string> $options what comes between `rector process` and drupdater's own options
     */
    public function __construct(
        private readonly ProcessRunner $runner,
        private readonly Bin $bin,
        private readonly string $config,
        private readonly string $projectDir,
        private readonly array $options = [],
    ) {
    }

    /**
     * Rector as the project installs it.
     */
    public static function project(ProcessRunner $runner, Bin $projectBin, string $config, string $projectDir): self
    {
        return new self($runner, $projectBin, $config, $projectDir);
    }

    /**
     * drupdater's own Rector, loading the project's autoloader, wherever vendor-dir puts it.
     */
    public static function bundled(ProcessRunner $runner, Bin $toolsBin, string $config, string $projectDir, string $vendorDir): self
    {
        return new self($runner, $toolsBin, $config, $projectDir, ['--autoload-file', $vendorDir . '/autoload.php']);
    }

    /**
     * Rewrites the directories and returns Rector's JSON report: the changed files, and each
     * file's diff with the rules that fired.
     *
     * @param list<string> $directories relative to the project
     * @return array{changed_files: list<string>, file_diffs: list<array{file: string, applied_rectors: list<string>}>}
     *
     * @throws Failure when Rector fails or prints no report
     */
    public function process(array $directories): array
    {
        if ($directories === []) {
            return ['changed_files' => [], 'file_diffs' => []];
        }

        // No --no-diffs: Rector's JSON lists each file's applied rules only alongside its diff.
        $result = $this->runner->run(
            [...$this->bin->command('rector'), 'process', ...$this->options, '--config=' . $this->config, '--no-progress-bar', '--debug', '--output-format=json', ...$directories],
            $this->projectDir,
            $this->bin->env(),
        );
        if (!$result->succeeded()) {
            throw ToolFailure::exited('rector', $result);
        }
        try {
            $report = json_decode($result->stdout, true, 512, JSON_THROW_ON_ERROR);
        }
        catch (\JsonException $e) {
            throw ToolFailure::unreadable('rector', $e);
        }

        $diffs = [];
        foreach ((array) ($report['file_diffs'] ?? []) as $diff) {
            $file = (string) ($diff['file'] ?? '');
            $this->restoreFileDocBlock($file);
            $diffs[] = ['file' => $file, 'applied_rectors' => array_values(array_map('strval', (array) ($diff['applied_rectors'] ?? [])))];
        }
        return ['changed_files' => array_values(array_map('strval', (array) ($report['changed_files'] ?? []))), 'file_diffs' => $diffs];
    }

    private function restoreFileDocBlock(string $file): void
    {
        $path = str_starts_with($file, '/') ? $file : $this->projectDir . '/' . $file;
        $content = @file_get_contents($path);
        if ($content === false) {
            throw new Failure(Failure::FAILED, 'a file rector changed could not be read', $path);
        }
        $fixed = FileDocBlock::first($content);
        if ($fixed !== $content && @file_put_contents($path, $fixed) === false) {
            throw new Failure(Failure::FAILED, 'the @file docblock could not be restored', $path);
        }
    }
}
