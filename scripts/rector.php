<?php

declare(strict_types=1);

use Rector\Config\RectorConfig;
use DrupalRector\Rector\Deprecation\DeprecationHelperRemoveRector;
use DrupalRector\Rector\ValueObject\DeprecationHelperRemoveConfiguration;
use DrupalRector\Set\Drupal10SetList;
use DrupalRector\Set\DrupalSetProvider;

if (class_exists('DrupalFinder\DrupalFinderComposerRuntime')) {
  $drupalFinder = new DrupalFinder\DrupalFinderComposerRuntime();
}
else {
  $drupalFinder = new DrupalFinder\DrupalFinder();
  $drupalFinder->locateRoot(getcwd());
}
$drupalRoot = $drupalFinder->getDrupalRoot();

// phpstan-drupal types \Drupal::service() calls from the container's service map; without
// it Rector cannot see a deprecated method called on one. Located through its class, so the
// copy found is the one this Rector's autoloader loads: bundled, or the project's own.
$phpstanConfigs = [];
$bootstrapFiles = [];
if (class_exists('mglaman\PHPStanDrupal\Drupal\DrupalAutoloader')) {
  $phpstanDrupal = dirname((new ReflectionClass('mglaman\PHPStanDrupal\Drupal\DrupalAutoloader'))->getFileName(), 3);
  $phpstanConfigs[] = $phpstanDrupal . '/extension.neon';
  // Rector runs only its own bootstrap files, not an extension's; this builds the service map.
  $bootstrapFiles[] = __DIR__ . '/phpstan-drupal-bootstrap.php';
}

// Composer-based sets read the deprecation sets from the installed drupal/core
// version, replacing the manual vendor-directory walk.
return RectorConfig::configure()
  ->withPHPStanConfigs($phpstanConfigs)
  ->withBootstrapFiles($bootstrapFiles)
  ->withSetProviders(DrupalSetProvider::class)
  ->withComposerBased(drupal: TRUE)
  // Composer-based sets match the installed major only; 10.x deprecations stay live until 12.
  ->withSets([Drupal10SetList::DRUPAL_10])
  ->withConfiguredRule(DeprecationHelperRemoveRector::class, [
    new DeprecationHelperRemoveConfiguration(\Drupal::VERSION),
  ])
  ->withAutoloadPaths([
    $drupalRoot . '/core',
    $drupalRoot . '/modules',
    $drupalRoot . '/profiles',
    $drupalRoot . '/themes',
  ])
  ->withFileExtensions(
    ['php', 'module', 'theme', 'install', 'profile', 'inc', 'engine']
  )
  ->withImportNames(
    importNames: TRUE,
    importDocBlockNames: FALSE,
    importShortClasses: FALSE,
    removeUnusedImports: FALSE
  );
