<?php

declare(strict_types=1);

use Rector\Config\RectorConfig;
use DrupalRector\Rector\Deprecation\DeprecationHelperRemoveRector;
use DrupalRector\Rector\ValueObject\DeprecationHelperRemoveConfiguration;
use DrupalRector\Set\Drupal10SetList;
use DrupalRector\Set\Drupal8SetList;
use DrupalRector\Set\Drupal9SetList;

if (class_exists('DrupalFinder\DrupalFinderComposerRuntime')) {
  $drupalFinder = new DrupalFinder\DrupalFinderComposerRuntime();
}
else {
  $drupalFinder = new DrupalFinder\DrupalFinder();
  $drupalFinder->locateRoot(getcwd());
}
$drupalRoot = $drupalFinder->getDrupalRoot();

// Every set, as drupal-rector advises: each rule checks the installed core version itself.
return RectorConfig::configure()
  ->withSets([
    Drupal8SetList::DRUPAL_8,
    Drupal9SetList::DRUPAL_9,
    Drupal10SetList::DRUPAL_10,
  ])
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
