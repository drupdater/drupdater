<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\Addon;

use Drupdater\Agent\Addon\ComposerAllowPlugins;
use Drupdater\Agent\Addon\Event;
use Drupdater\Agent\Addon\Plan;
use Drupdater\Agent\Failure;
use Drupdater\Agent\Plugins\AllowPlugins;
use Drupdater\Agent\Tests\FixtureProject;
use Drupdater\Agent\Tests\InProject;
use PHPUnit\Framework\TestCase;

final class ComposerAllowPluginsTest extends TestCase
{
    use InProject;
    use FixtureProject;

    public function testOpensBeforeTheUpdateAndClosesAfterWithWhatOpeningRecorded(): void
    {
        $project = $this->fixtureProject();

        $opened = $this->addon($project)->handle(Event::preComposerUpdate($project, new Plan()));
        self::assertNull($opened->plan, 'it proposes nothing about the update');
        self::assertSame([], $opened->commits);

        $closed = $this->addon($project)->handle(Event::postComposerUpdate($project, [], Records::findings([
            Records::record('pre-composer-update', $opened->finding),
        ])));

        self::assertSame(['dealerdirect/phpcodesniffer-composer-installer'], $closed->finding);
        self::assertEquals(
            (object) ['cweagans/composer-patches' => true, 'drupal/*' => false, 'dealerdirect/phpcodesniffer-composer-installer' => false],
            $this->manifest($project)->config->{'allow-plugins'},
        );
    }

    public function testAnEmptySettingRoundTripsAsAnEmptyObject(): void
    {
        $project = $this->fixtureProject(['allow-plugins' => new \stdClass()], withoutPlugins: true);

        $opened = $this->addon($project)->handle(Event::preComposerUpdate($project, new Plan()));
        $this->addon($project)->handle(Event::postComposerUpdate($project, [], Records::findings([Records::record('pre-composer-update', $opened->finding)])));

        self::assertEquals(new \stdClass(), $this->manifest($project)->config->{'allow-plugins'});
    }

    public function testRefusesToCloseWithoutItsOpeningAndWritesNothing(): void
    {
        $project = $this->fixtureProject();
        $before = file_get_contents($project . '/composer.json');

        try {
            $this->addon($project)->handle(Event::postComposerUpdate($project, []));
            self::fail('closed without its opening');
        }
        catch (Failure $failure) {
            self::assertSame(Failure::INVALID_REQUEST, $failure->kind);
        }
        self::assertSame($before, file_get_contents($project . '/composer.json'));
    }

    public function testAnOpeningRecordedAsSomethingElseIsTheAgentsOwnFault(): void
    {
        $project = $this->fixtureProject();

        $this->expectException(\LogicException::class);
        $this->addon($project)->handle(Event::postComposerUpdate($project, [], Records::findings([
            Records::record('pre-composer-update', ['original' => 'all', 'opened' => true]),
        ])));
    }

    public function testTheReportListsTheDeniedPluginsSorted(): void
    {
        self::assertSame(['added' => ['php-http/discovery', 'tbachert/spi']], ComposerAllowPlugins::report(Records::findings([
            Records::record('post-composer-update', ['tbachert/spi', 'php-http/discovery']),
        ])));
        self::assertNull(ComposerAllowPlugins::report(Records::findings([Records::record('post-composer-update', [])])));
        self::assertNull(ComposerAllowPlugins::report(Records::findings()));
    }

    private function addon(string $project): ComposerAllowPlugins
    {
        return new ComposerAllowPlugins(new AllowPlugins($this->composer($project), $project . '/composer.json'));
    }
}
