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

// Composer-based sets read the deprecation sets from the installed drupal/core
// version, replacing the manual vendor-directory walk.
return RectorConfig::configure()
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
