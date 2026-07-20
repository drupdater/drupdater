<?php

declare(strict_types=1);

use Rector\Config\RectorConfig;
use DrupalRector\Rector\Deprecation\DeprecationHelperRemoveRector;
use DrupalRector\Rector\ValueObject\DeprecationHelperRemoveConfiguration;
use DrupalRector\Set\DrupalSetProvider;

if (class_exists('DrupalFinder\DrupalFinderComposerRuntime')) {
  $drupalFinder = new DrupalFinder\DrupalFinderComposerRuntime();
}
else {
  $drupalFinder = new DrupalFinder\DrupalFinder();
  $drupalFinder->locateRoot(__DIR__);
}
$drupalRoot = $drupalFinder->getDrupalRoot();

// Composer-based sets pick the deprecation sets to run from the installed
// `drupal/core` version, so the manual vendor-directory walk to build a set
// list is no longer needed.
return RectorConfig::configure()
  ->withSetProviders(DrupalSetProvider::class)
  ->withComposerBased(drupal: TRUE)
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
