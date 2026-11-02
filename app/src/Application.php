<?php

declare(strict_types=1);

namespace Drupdater\App;

use Drupdater\App\Agent\Agent;
use Drupdater\App\Command\CommitCommand;
use Drupdater\App\Command\FireCommand;
use Drupdater\App\Git\Committer;
use Drupdater\App\Git\Git;
use Drupdater\App\Git\GitCommitter;
use Drupdater\App\Git\Identity;
use Drupdater\App\Pipeline\AgentHooks;
use Symfony\Component\Console\Application as Console;

/**
 * The composition root: every command with its real dependencies. One process runs one command,
 * so they share one log.
 */
final class Application
{
    public static function create(): Console
    {
        $log = new Log();
        $committer = static fn (string $dir, Identity $author): Committer => new GitCommitter(new Git($dir), $author);
        $console = new Console('drupdater');
        $console->addCommand(new FireCommand($log, new AgentHooks(new Agent($log)), $committer));
        $console->addCommand(new CommitCommand($log, $committer));
        return $console;
    }
}
