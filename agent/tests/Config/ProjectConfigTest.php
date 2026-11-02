<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\Config;

use Drupdater\Agent\Addon\Catalogue;
use Drupdater\Agent\Addon\CatalogueEntry;
use Drupdater\Agent\Config\ProjectConfig;
use Drupdater\Agent\Failure;
use Drupdater\Agent\Tests\Addon\ScriptedAddon;
use Drupdater\Agent\Tests\Fails;
use Drupdater\Agent\Tests\Scratch;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;

final class ProjectConfigTest extends TestCase
{
    use Fails;
    use Scratch;

    private const MANDATORY = ['composer_allow_plugins', 'composer_patches', 'composer_diff', 'update_hooks', 'composer_audit', 'unsupported_modules'];
    private const DEFAULTS = ['code_beautifier', 'deprecations_remover', 'translations_updater', 'composer_normalizer'];

    private string $path;

    public function testAMissingFileAppliesTheDefaults(): void
    {
        $config = ProjectConfig::load($this->scratchDir() . '/.drupdater.yaml', Catalogue::builtin());

        self::assertSame([
            'found' => false,
            'sites' => ['default'],
            'timeout_ns' => 1_800_000_000_000,
            'run_types' => [
                'normal' => ['addons' => [...self::MANDATORY, ...self::DEFAULTS], 'auto_merge' => false],
                'security' => ['addons' => self::MANDATORY, 'auto_merge' => false],
            ],
        ], $config->toArray());
    }

    /**
     * @return iterable<string, array{string}>
     */
    public static function noDocument(): iterable
    {
        yield 'empty' => [''];
        yield 'comments only' => ["# everything is commented out\n# sites: [a]\n"];
        yield 'an empty document' => ["---\n"];
    }

    #[DataProvider('noDocument')]
    public function testAFileWithNoDocumentAppliesTheDefaultsButIsFound(string $yaml): void
    {
        $config = $this->load($yaml);

        self::assertTrue($config->found);
        self::assertSame(['default'], $config->sites);
        self::assertSame(1_800_000_000_000, $config->timeoutNanoseconds);
        self::assertSame([...self::MANDATORY, ...self::DEFAULTS], $config->runTypes['normal']['addons']);
    }

    public function testAPartialFileKeepsTheDefaultsForAbsentKeys(): void
    {
        $config = $this->load("run_types:\n  normal:\n    addons: [code_beautifier]\n");

        self::assertSame([...self::MANDATORY, 'code_beautifier'], $config->runTypes['normal']['addons']);
        self::assertSame(self::MANDATORY, $config->runTypes['security']['addons']);
        self::assertSame(['default'], $config->sites);
        self::assertSame(1_800_000_000_000, $config->timeoutNanoseconds);
    }

    public function testAnEmptyAddonListNamesNoneWhereAnAbsentOneTakesTheCataloguesDefaults(): void
    {
        self::assertSame(self::MANDATORY, $this->load("run_types:\n  normal:\n    addons: []\n")->runTypes['normal']['addons']);
        self::assertSame([...self::MANDATORY, ...self::DEFAULTS], $this->load("run_types:\n  normal:\n    addons:\n")->runTypes['normal']['addons']);
    }

    public function testASecurityRunGetsWhatItNamesAfterTheMandatoryAddons(): void
    {
        $config = $this->load("run_types:\n  security:\n    addons: [composer_normalizer, composer_diff]\n    auto_merge: true\n");

        self::assertSame(['addons' => [...self::MANDATORY, 'composer_normalizer'], 'auto_merge' => true], $config->runTypes['security']);
        self::assertFalse($config->runTypes['normal']['auto_merge'], 'auto_merge is per run type');
    }

    public function testTheMandatoryAddonsComeFirstInCatalogueOrderThenTheConfiguredInFileOrder(): void
    {
        $catalogue = new Catalogue([
            new CatalogueEntry(ScriptedAddon::class, enabledByDefault: true),
            new CatalogueEntry(NamedAddon::class, mandatory: true),
        ]);
        $this->path = $this->scratchDir() . '/.drupdater.yaml';
        file_put_contents($this->path, "run_types:\n  security:\n    addons: [scripted, named, scripted]\n");

        $config = ProjectConfig::load($this->path, $catalogue);

        self::assertSame(['named', 'scripted'], $config->runTypes['normal']['addons'], 'the defaults when it names none');
        self::assertSame(['named', 'scripted'], $config->runTypes['security']['addons'], 'each named once');
    }

    /**
     * @return iterable<string, array{string, int}>
     */
    public static function timeouts(): iterable
    {
        yield 'a duration' => ["timeout: 90s\n", 90_000_000_000];
        yield 'zero disables it' => ["timeout: 0\n", 0];
        yield 'two units' => ["timeout: 1h30m\n", 5_400_000_000_000];
        yield 'quoted' => ["timeout: \"2h\"\n", 7_200_000_000_000];
        yield 'left blank keeps the default' => ["timeout:\n", 1_800_000_000_000];
    }

    #[DataProvider('timeouts')]
    public function testTheTimeoutIsAGoDuration(string $yaml, int $nanoseconds): void
    {
        self::assertSame($nanoseconds, $this->load($yaml)->timeoutNanoseconds);
    }

    public function testSitesAreAppliedInOrderAndNumbersReadAsNames(): void
    {
        self::assertSame(['default', 'subsite_a', '2'], $this->load("sites: [default, subsite_a, 2]\n")->sites);
    }

    public function testYamlOneOneBooleansStillReadAsBooleans(): void
    {
        $config = $this->load("run_types:\n  normal:\n    auto_merge: yes\n  security:\n    auto_merge: Off\n");

        self::assertTrue($config->runTypes['normal']['auto_merge']);
        self::assertFalse($config->runTypes['security']['auto_merge']);
    }

    /**
     * @return iterable<string, array{string, string}>
     */
    public static function invalid(): iterable
    {
        yield 'an unknown key' => ["timout: 30m\n", 'parsing %s: unknown key "timout"'];
        yield 'an unknown run type' => ["run_types:\n  weekly: {}\n", 'parsing %s: unknown key "run_types.weekly"'];
        yield 'an unknown run type key' => ["run_types:\n  normal:\n    addon: []\n", 'parsing %s: unknown key "run_types.normal.addon"'];
        yield 'a numeric key' => ["1: x\n", 'parsing %s: unknown key "1"'];
        yield 'sites that are no list' => ["sites: default\n", 'parsing %s: "sites" must be a list'];
        yield 'sites that are a mapping' => ["sites: {a: 1}\n", 'parsing %s: "sites" must be a list'];
        yield 'a site that is no name' => ["sites: [[a]]\n", 'parsing %s: "sites" must be a list of names'];
        yield 'addons that are no list' => ["run_types:\n  normal:\n    addons: code_beautifier\n", 'parsing %s: "run_types.normal.addons" must be a list'];
        yield 'run types that are a list' => ["run_types: [normal]\n", 'parsing %s: "run_types" must be a mapping of keys'];
        yield 'a run type that is a string' => ["run_types:\n  normal: all\n", 'parsing %s: "run_types.normal" must be a mapping of keys'];
        yield 'auto_merge that is a word' => ["run_types:\n  normal:\n    auto_merge: always\n", 'parsing %s: "run_types.normal.auto_merge" must be true or false'];
        yield 'auto_merge that is quoted' => ["run_types:\n  normal:\n    auto_merge: \"true\"\n", 'parsing %s: "run_types.normal.auto_merge" must be true or false'];
        yield 'a timeout that is a list' => ["timeout: [30m]\n", 'parsing %s: "timeout" must be a single value'];
        yield 'a document that is a list' => ["- a\n", 'parsing %s: the file must be a mapping of keys, such as "sites:"'];
        yield 'a document that is a string' => ["just a string\n", 'parsing %s: the file must be a mapping of keys, such as "sites:"'];
        yield 'an unparseable timeout' => ["timeout: 30 minutes\n", 'in %s: invalid timeout "30 minutes" (use a Go duration like "30m" or "2h", or 0 to disable): time: unknown unit " minutes" in duration "30 minutes"'];
        yield 'a decimal timeout' => ["timeout: 1.5\n", 'in %s: invalid timeout "1.5" (use a Go duration like "30m" or "2h", or 0 to disable): time: missing unit in duration "1.5"'];
        yield 'a whole decimal timeout' => ["timeout: 0.0\n", 'in %s: invalid timeout "0.0" (use a Go duration like "30m" or "2h", or 0 to disable): time: missing unit in duration "0.0"'];
        yield 'a boolean timeout' => ["timeout: true\n", 'in %s: invalid timeout "true" (use a Go duration like "30m" or "2h", or 0 to disable): time: invalid duration "true"'];
        yield 'an empty timeout' => ["timeout: \"\"\n", 'in %s: invalid timeout "" (use a Go duration like "30m" or "2h", or 0 to disable): time: invalid duration ""'];
        yield 'no sites' => ["sites: []\n", 'in %s: no sites configured: "sites" must list at least one Drupal site name'];
        yield 'sites left blank' => ["sites:\n", 'in %s: no sites configured: "sites" must list at least one Drupal site name'];
        yield 'an empty site' => ["sites: [\"\"]\n", 'in %s: site "" is no directory name under sites/'];
        yield 'a site path' => ["sites: [a/b]\n", 'in %s: site "a/b" is no directory name under sites/'];
        yield 'the parent site' => ["sites: [..]\n", 'in %s: site ".." is no directory name under sites/'];
        yield 'a site twice' => ["sites: [a, b, a]\n", 'in %s: site "a" is listed twice'];
        yield 'a key twice' => ["sites: [a]\nsites: [b]\n", 'parsing %s: Duplicate key "sites" detected at line 2 (near "sites: [b]").'];
        yield 'the old layout' => ["addons:\n  normal: []\n", "in %s: \"addons\" and \"auto_merge\" are now grouped per run type. Replace:\n"];
        yield 'the old auto_merge' => ["auto_merge: true\nsites: []\n", "in %s: \"addons\" and \"auto_merge\" are now grouped per run type. Replace:\n"];
        yield 'an unknown addon' => ["run_types:\n  security:\n    addons: [code_beautifer]\n", 'unknown addon "code_beautifer" (run "drupdater addons" to list valid names)'];
    }

    #[DataProvider('invalid')]
    public function testAnInvalidFileIsRefusedSayingWhatToFix(string $yaml, string $reason): void
    {
        $failure = self::assertFailsWith(fn () => $this->load($yaml));

        $prefix = sprintf($reason, $this->path);
        self::assertNotSame('', $prefix);
        self::assertSame(Failure::FAILED, $failure->kind);
        self::assertStringStartsWith($prefix, $failure->reason);
    }

    public function testAnUnknownAddonInTheBlockThisRunDoesNotUseStillFailsIt(): void
    {
        $failure = self::assertFailsWith(fn () => $this->load("run_types:\n  normal:\n    addons: [code_beautifier]\n  security:\n    addons: [nope]\n"));

        self::assertSame('unknown addon "nope" (run "drupdater addons" to list valid names)', $failure->reason);
    }

    public function testTheOldLayoutIsRefusedWithTheWholeReplacement(): void
    {
        $failure = self::assertFailsWith(fn () => $this->load("addons:\n  normal: []\n"));

        self::assertStringEndsWith("with:\n  run_types:\n    normal:\n      addons: [code_beautifier, ...]\n      auto_merge: false\n    security:\n      addons: []\n      auto_merge: true", $failure->reason);
    }

    public function testUnparseableYamlIsRefused(): void
    {
        $failure = self::assertFailsWith(fn () => $this->load("sites: [a\n"));

        self::assertStringStartsWith(sprintf('parsing %s: ', $this->path), $failure->reason);
    }

    public function testADirectoryIsNoFileToRead(): void
    {
        $dir = $this->scratchDir();

        self::assertFails(Failure::FAILED, 'failed to read ' . $dir, static fn () => ProjectConfig::load($dir, Catalogue::builtin()));
    }

    /**
     * @return iterable<string, array{string}>
     */
    public static function hostile(): iterable
    {
        yield 'a nul byte' => ["\x00"];
        yield 'invalid UTF-8' => ["sites: [\xff\xfe]\n"];
        yield 'a tab indent' => ["run_types:\n\tnormal: {}\n"];
        yield 'a PHP object tag' => ["sites: !php/object 'O:8:\"stdClass\":0:{}'\n"];
        yield 'a PHP constant tag' => ["timeout: !php/const PHP_INT_MAX\n"];
        yield 'a custom tag' => ["sites: !custom [a]\n"];
        yield 'a date' => ["timeout: 2001-12-14\n"];
        yield 'an anchor and alias' => ["sites: &s [a]\nrun_types:\n  normal:\n    addons: *s\n"];
        yield 'a merge key' => ["run_types:\n  normal: &n {auto_merge: true}\n  security:\n    <<: *n\n"];
        yield 'deep nesting' => [str_repeat('[', 200) . str_repeat(']', 200)];
        yield 'a binary scalar' => ["timeout: !!binary aGVsbG8=\n"];
        yield 'infinity' => ["timeout: .inf\n"];
    }

    /**
     * The file is the project's, so whatever it holds is either a configuration or a reason to fix
     * it, never the agent failing.
     */
    #[DataProvider('hostile')]
    public function testWhateverTheFileHoldsItIsReadOrRefusedAsAFailedConfig(string $yaml): void
    {
        try {
            $config = $this->load($yaml);
        }
        catch (Failure $failure) {
            self::assertSame(Failure::FAILED, $failure->kind, $failure->reason);
            return;
        }
        self::assertNotSame([], $config->sites);
    }

    public function testEveryKeyRoundTrips(): void
    {
        $config = $this->load(<<<'YAML'
            sites: [default, second]
            timeout: 2h
            run_types:
              normal:
                addons: [code_beautifier]
                auto_merge: false
              security:
                addons: []
                auto_merge: true

            YAML);

        self::assertSame([
            'found' => true,
            'sites' => ['default', 'second'],
            'timeout_ns' => 7_200_000_000_000,
            'run_types' => [
                'normal' => ['addons' => [...self::MANDATORY, 'code_beautifier'], 'auto_merge' => false],
                'security' => ['addons' => self::MANDATORY, 'auto_merge' => true],
            ],
        ], $config->toArray());
    }

    private function load(string $yaml): ProjectConfig
    {
        $this->path = $this->scratchDir() . '/.drupdater.yaml';
        file_put_contents($this->path, $yaml);
        return ProjectConfig::load($this->path, Catalogue::builtin());
    }

    private static function assertFailsWith(\Closure $run): Failure
    {
        try {
            $run();
        }
        catch (Failure $failure) {
            return $failure;
        }
        self::fail('no failure');
    }
}
