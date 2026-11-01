<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\Addon;

use Composer\Console\Application;
use Composer\IO\NullIO;
use Composer\Policy\PolicyConfig;
use Drupdater\Agent\Addon\Catalogue;
use Drupdater\Agent\Addon\Services;
use Drupdater\Agent\DrupalOrg\DrupalCode;
use Drupdater\Agent\Failure;
use Drupdater\Agent\Log;
use Drupdater\Agent\Resolve\Resolver;
use Drupdater\Agent\Tests\FixtureProject;
use Drupdater\Agent\Tests\InProject;
use PHPUnit\Framework\TestCase;
use Symfony\Component\Console\Output\NullOutput;

/**
 * The real tools every addon is built from, for the fixture project.
 */
final class ServicesTest extends TestCase
{
    use InProject {
        setUp as projectSetUp;
        tearDown as projectTearDown;
    }
    use FixtureProject;

    private string|false $token;
    private string|false $binary;

    protected function setUp(): void
    {
        $this->projectSetUp();
        $this->token = getenv(DrupalCode::TOKEN_VARIABLE);
        $this->binary = getenv('COMPOSER_BINARY');
    }

    protected function tearDown(): void
    {
        putenv($this->token === false ? DrupalCode::TOKEN_VARIABLE : DrupalCode::TOKEN_VARIABLE . '=' . $this->token);
        putenv($this->binary === false ? 'COMPOSER_BINARY' : 'COMPOSER_BINARY=' . $this->binary);
        $this->projectTearDown();
    }

    public function testEveryAddonOfTheCatalogueBuildsFromTheRealTools(): void
    {
        putenv('COMPOSER_BINARY=/usr/bin/composer');
        $services = $this->services($this->fixtureProject());

        foreach (Catalogue::builtin()->entries() as $entry) {
            self::assertInstanceOf($entry->addon, $entry->addon::create($services));
        }
    }

    public function testTheProjectIsReadFromTheRunningComposersProject(): void
    {
        $project = $this->fixtureProject();
        $services = $this->services($project);

        self::assertSame(realpath($project), $services->projectDir());
        self::assertSame('web', $services->project()->webRoot);
        self::assertTrue($services->project()->installed('cweagans/composer-patches'));
        self::assertEquals((object) [
            'Issue #3123456: [Token fix](https://www.drupal.org/i/3123456)' => 'patches/token/3123456-local.patch',
            'Remote fix' => 'https://www.drupal.org/files/issues/3200000-8.patch',
        ], $services->project()->patches->{'drupal/token'});
    }

    public function testTheProjectIsReadAfreshEachTime(): void
    {
        $project = $this->fixtureProject();
        $services = $this->services($project);
        $before = $services->project();

        $manifest = $this->manifest($project);
        unset($manifest->extra->patches);
        file_put_contents($project . '/composer.json', json_encode($manifest));

        self::assertNotNull($before->patches);
        self::assertNull($services->project()->patches);
    }

    public function testDrupalCodeNeedsItsToken(): void
    {
        $services = $this->services($this->fixtureProject());

        putenv(DrupalCode::TOKEN_VARIABLE);
        self::assertNull($services->drupalCode());
        putenv(DrupalCode::TOKEN_VARIABLE . '=secret');
        self::assertInstanceOf(DrupalCode::class, $services->drupalCode());
    }

    public function testComposerCannotBeRunAgainWithoutKnowingWhatItWasStartedAs(): void
    {
        putenv('COMPOSER_BINARY');

        $this->expectExceptionObject(new Failure(Failure::INTERNAL, 'COMPOSER_BINARY is not set, so Composer cannot be run again'));
        $this->services($this->fixtureProject())->composerCli();
    }

    public function testTheRectorConfigShipsWithTheAgent(): void
    {
        self::assertFileExists(Services::rectorConfig());
        self::assertFileExists(dirname(Services::rectorConfig()) . '/phpstan-drupal-bootstrap.php', 'rector.php includes it from beside itself');
    }

    private function services(string $project): Services
    {
        $composer = $this->composer($project);
        $application = new Application();
        $application->setAutoExit(false);
        return new Services(
            $composer,
            new NullIO(),
            new Resolver($application->find('update'), new NullOutput()),
            static fn (): PolicyConfig => PolicyConfig::fromConfig($composer->getConfig()),
            new Log(),
        );
    }
}
