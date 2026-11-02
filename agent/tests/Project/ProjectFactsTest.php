<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\Project;

use Composer\Console\Application;
use Drupdater\Agent\Command\ProjectCommand;
use Drupdater\Agent\Project\Project;
use Drupdater\Agent\Project\ProjectFacts;
use Drupdater\Agent\Tests\FixtureProject;
use Drupdater\Agent\Tests\InProject;
use PHPUnit\Framework\TestCase;
use Symfony\Component\Console\Tester\CommandTester;

final class ProjectFactsTest extends TestCase
{
    use InProject;
    use FixtureProject;

    public function testTheCommandAnswersTheFixtureForTheFixtureProject(): void
    {
        $project = $this->fixtureProject();
        $command = new ProjectCommand();
        $command->setComposer($this->composer($project));
        $application = new Application();
        $application->setAutoExit(false);
        $application->add($command);
        $tester = new CommandTester($command);
        $tester->setInputs([json_encode(self::fixture('project/request.json'))]);

        self::assertSame(0, $tester->execute([]));
        self::assertEquals(self::fixture('project/response.json'), json_decode($tester->getDisplay(), true));
    }

    public function testReadsTheFixtureProject(): void
    {
        $project = $this->read($this->fixtureProject());

        self::assertSame('web', $project->webRoot);
        self::assertSame(['web/modules/custom', 'web/themes/custom'], $project->customCodeDirectories);
        self::assertSame('4b2f0e1c9a7d3e5f6a8b0c1d2e3f4a5b', $project->lockHash);
        self::assertSame([
            'cweagans/composer-patches' => '1.7.3',
            'dealerdirect/phpcodesniffer-composer-installer' => 'v1.0.0',
            'drupal/core' => '10.3.1',
            'drupal/devel' => 'dev-5.x',
            'drupal/recipe_kit' => '1.0.0',
            'drupal/token' => '1.15.0',
        ], $project->packages);
        self::assertTrue($project->installed('drupal/devel'), 'dev packages count');
        self::assertSame('10.3.1', $project->version('drupal/core'));
        self::assertSame('', $project->version('drupal/missing'));
        self::assertSame(['drupal/core' => [
            'https://www.drupal.org/files/issues/3100000-1.patch',
            'https://www.drupal.org/files/issues/3100001-2.patch',
        ]], $project->dependencyPatches);
    }

    public function testKeepsThePatchesAsWrittenEmptyObjectsIncluded(): void
    {
        $patches = $this->read($this->fixtureProject())->patches;

        self::assertInstanceOf(\stdClass::class, $patches);
        self::assertSame(['drupal/token', 'drupal/empty'], array_keys((array) $patches));
        self::assertEquals(new \stdClass(), $patches->{'drupal/empty'});
    }

    public function testAProjectWithoutALockHasNoPackages(): void
    {
        $project = $this->fixtureProject();
        unlink($project . '/composer.lock');

        $read = $this->read($project);

        self::assertNull($read->lockHash);
        self::assertSame([], $read->packages);
        self::assertSame([], $read->dependencyPatches);
        self::assertSame('web', $read->webRoot);
    }

    public function testLeavesOutWhatComposerJsonDoesNotSay(): void
    {
        $project = $this->scratchDir();
        $this->writeFiles($project, ['composer.json' => '{"name": "fixture/bare"}', 'web/modules/custom/x.module' => '']);

        $read = $this->read($project);

        self::assertNull($read->webRoot);
        self::assertSame([], $read->customCodeDirectories);
        self::assertNull($read->patches);
    }

    public function testFindsCustomProfilesUnderAnotherWebRoot(): void
    {
        $project = $this->scratchDir();
        $this->writeFiles($project, ['composer.json' => json_encode(['extra' => ['drupal-scaffold' => ['locations' => ['web-root' => 'docroot/']]]])]);
        mkdir($project . '/docroot/profiles/custom', 0777, true);

        $read = $this->read($project);

        self::assertSame('docroot', $read->webRoot);
        self::assertSame(['docroot/profiles/custom'], $read->customCodeDirectories);
    }

    public function testAnEmptyWebRootIsNoWebRoot(): void
    {
        $project = $this->scratchDir();
        $this->writeFiles($project, ['composer.json' => '{"extra": {"drupal-scaffold": {"locations": {"web-root": ""}}}}']);

        self::assertNull($this->read($project)->webRoot);
    }

    private function read(string $project): Project
    {
        return (new ProjectFacts($this->composer($project), $project . '/composer.json'))->project();
    }
}
