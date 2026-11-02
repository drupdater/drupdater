<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tools;

use Drupdater\Agent\Failure;
use Drupdater\Agent\Json;
use Drupdater\Agent\Process\ProcessRunner;

/**
 * PHP_CodeSniffer, run over what the project's phpcs.xml covers.
 */
class Phpcs
{
    public function __construct(private readonly ProcessRunner $runner, private readonly Bin $bin, private readonly string $projectDir)
    {
    }

    /**
     * The violations phpcs finds, as its JSON report: files by path, each with its errors and
     * warnings, and the totals, fixable included.
     *
     * @return array{files: array<string, array{errors: int, warnings: int}>, totals: array{errors: int, warnings: int, fixable: int}}
     *
     * @throws Failure when phpcs fails or prints no report
     */
    public function report(): array
    {
        $result = $this->runner->run(
            [...$this->bin->command('phpcs'), '--report=json', '-q', '--runtime-set', 'ignore_errors_on_exit', '1', '--runtime-set', 'ignore_warnings_on_exit', '1'],
            $this->projectDir,
            $this->bin->env(),
        );
        if (!$result->succeeded()) {
            throw ToolFailure::exited('phpcs', $result);
        }
        try {
            $report = json_decode($result->stdout, true, 512, JSON_THROW_ON_ERROR);
        }
        catch (\JsonException $e) {
            throw ToolFailure::unreadable('phpcs', $e);
        }
        $files = [];
        foreach (Json::map(Json::at($report, 'files')) as $file => $counts) {
            $files[$file] = ['errors' => Json::int(Json::at($counts, 'errors')), 'warnings' => Json::int(Json::at($counts, 'warnings'))];
        }
        $totals = Json::at($report, 'totals');
        return [
            'files' => $files,
            'totals' => [
                'errors' => Json::int(Json::at($totals, 'errors')),
                'warnings' => Json::int(Json::at($totals, 'warnings')),
                'fixable' => Json::int(Json::at($totals, 'fixable')),
            ],
        ];
    }

    /**
     * Fixes what phpcbf can. It exits non-zero whenever a violation remains.
     *
     * @throws Failure when phpcbf exits non-zero
     */
    public function fix(): void
    {
        $result = $this->runner->run($this->bin->command('phpcbf'), $this->projectDir, $this->bin->env());
        if (!$result->succeeded()) {
            throw ToolFailure::exited('phpcbf', $result);
        }
    }
}
