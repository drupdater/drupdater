<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests;

use Composer\Composer;
use Composer\Factory;
use Composer\IO\NullIO;

/**
 * A test that needs Composer for a scratch project: its own COMPOSER_HOME, and the working
 * directory put back afterwards, as Composer reads the project from it.
 */
trait InProject
{
    use Scratch {
        tearDown as scratchTearDown;
    }

    private string|false $composerHome;
    private string $cwd;

    protected function setUp(): void
    {
        $this->composerHome = getenv('COMPOSER_HOME');
        putenv('COMPOSER_HOME=' . $this->scratchDir());
        $this->cwd = (string) getcwd();
    }

    protected function tearDown(): void
    {
        chdir($this->cwd);
        putenv($this->composerHome === false ? 'COMPOSER_HOME' : 'COMPOSER_HOME=' . $this->composerHome);
        $this->scratchTearDown();
    }

    protected function composer(string $project): Composer
    {
        chdir($project);
        return (new Factory())->createComposer(new NullIO(), $project . '/composer.json', true, $project);
    }
}
