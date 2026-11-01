<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\Addon;

use Drupdater\Agent\Addon\ComposerNormalizer;
use Drupdater\Agent\Addon\Event;
use Drupdater\Agent\Failure;
use Drupdater\Agent\Log;
use Drupdater\Agent\Project\Project;
use Drupdater\Agent\Tools\ComposerCli;
use PHPUnit\Framework\TestCase;

final class ComposerNormalizerTest extends TestCase
{
    public function testNormalizesWhereThePluginIsInstalled(): void
    {
        $composer = $this->createMock(ComposerCli::class);
        $composer->expects(self::once())->method('normalize');

        $result = $this->normalizer(new Project(packages: ['ergebnis/composer-normalize' => '2.0.0']), $composer)->handle(Event::postComposerUpdate('/p', []));

        self::assertNull($result->finding);
        self::assertSame([], $result->commits, 'what it rewrote is staged with the update itself');
    }

    public function testSkipsWithAWarningWhereItIsNot(): void
    {
        $log = new Log();
        $composer = $this->createMock(ComposerCli::class);
        $composer->expects(self::never())->method('normalize');

        (new ComposerNormalizer($log, static fn (): Project => new Project(), static fn (): ComposerCli => $composer))->handle(Event::postComposerUpdate('/p', []));

        self::assertSame([['level' => 'warning', 'message' => 'composer-normalize not installed, skipping', 'context' => []]], $log->entries());
    }

    public function testAFailedNormalizeFailsTheHook(): void
    {
        $composer = $this->createMock(ComposerCli::class);
        $composer->method('normalize')->willThrowException(new Failure(Failure::FAILED, 'composer normalize failed'));

        $this->expectExceptionObject(new Failure(Failure::FAILED, 'composer normalize failed'));
        $this->normalizer(new Project(packages: ['ergebnis/composer-normalize' => '2.0.0']), $composer)->handle(Event::postComposerUpdate('/p', []));
    }

    public function testItReportsNothing(): void
    {
        self::assertNull(ComposerNormalizer::report(Records::findings([Records::record('post-composer-update', null)])));
    }

    private function normalizer(Project $project, ComposerCli $composer): ComposerNormalizer
    {
        return new ComposerNormalizer(new Log(), static fn (): Project => $project, static fn (): ComposerCli => $composer);
    }
}
