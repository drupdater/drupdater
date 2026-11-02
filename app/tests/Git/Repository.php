<?php

declare(strict_types=1);

namespace Drupdater\App\Tests\Git;

use Symfony\Component\Process\Process;

/**
 * A real repository in a temporary directory, whose first commit tracks files, as a run gets one.
 */
final class Repository
{
    public readonly string $dir;

    /**
     * @param array<string, string> $files content by path
     */
    public function __construct(array $files = [])
    {
        $this->dir = sys_get_temp_dir() . '/drupdater-app-' . bin2hex(random_bytes(6));
        mkdir($this->dir);
        $this->git('init', '--quiet', '--initial-branch=main');
        foreach ($files as $path => $content) {
            $this->write($path, $content);
        }
        $this->git('add', '--all');
        $this->git('commit', '--quiet', '--allow-empty', '-m', 'init');
    }

    public function __destruct()
    {
        (new Process(['rm', '-rf', $this->dir]))->run();
    }

    /**
     * Runs git as a test fixture would, with an identity of its own; its output, trimmed.
     */
    public function git(string ...$args): string
    {
        $process = new Process(['git', '-C', $this->dir, ...array_values($args)], null, [
            'GIT_AUTHOR_NAME' => 't', 'GIT_AUTHOR_EMAIL' => 't@t', 'GIT_COMMITTER_NAME' => 't', 'GIT_COMMITTER_EMAIL' => 't@t',
            'GIT_CONFIG_NOSYSTEM' => '1', 'HOME' => $this->dir . '/.home',
        ]);
        $process->mustRun();
        return trim($process->getOutput());
    }

    public function write(string $path, string $content): void
    {
        $file = $this->dir . '/' . $path;
        if (!is_dir(dirname($file))) {
            mkdir(dirname($file), 0o777, true);
        }
        file_put_contents($file, $content);
    }

    public function remove(string $path): void
    {
        unlink($this->dir . '/' . $path);
    }

    /**
     * HEAD's message and the files it changed relative to its parent, sorted.
     *
     * @return array{string, list<string>}
     */
    public function head(): array
    {
        $files = array_values(array_filter(explode("\n", $this->git('diff-tree', '--no-commit-id', '--name-only', '-r', 'HEAD'))));
        sort($files);
        return [$this->git('log', '-1', '--format=%B'), $files];
    }

    /**
     * @return list<string>
     */
    public function staged(): array
    {
        return array_values(array_filter(explode("\n", $this->git('diff', '--cached', '--name-only'))));
    }
}
