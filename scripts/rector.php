<?php

declare(strict_types=1);

use Rector\Config\RectorConfig;
use DrupalRector\Rector\Deprecation\DeprecationHelperRemoveRector;
use DrupalRector\Rector\ValueObject\DeprecationHelperRemoveConfiguration;

return static function(RectorConfig $rectorConfig): void {
  if (class_exists('DrupalFinder\DrupalFinderComposerRuntime')) {
    $drupalFinder = new DrupalFinder\DrupalFinderComposerRuntime();
  }
  else {
    $drupalFinder = new DrupalFinder\DrupalFinder();
    $drupalFinder->locateRoot(__DIR__);
  }

  [$major, $minor] = explode('.', \Drupal::VERSION);
  $setLists = [];
  for ($i = 0; $i <= $minor; $i++) {
    $fileName = $drupalFinder->getVendorDir() . "/palantirnet/drupal-rector/config/drupal-$major/drupal-$major.$i-deprecations.php";
    if (file_exists($fileName)) {
      $setLists[] = $fileName;
    }
  }
  for ($i = 8; $i < $major; $i++) {
    $fileName = $drupalFinder->getVendorDir() . "/palantirnet/drupal-rector/config/drupal-$i/drupal-$i-all-deprecations.php";
    if (file_exists($fileName)) {
      $setLists[] = $fileName;
    }
  }
  $rectorConfig->sets($setLists);

  $rectorConfig->ruleWithConfiguration(DeprecationHelperRemoveRector::class, [
    new DeprecationHelperRemoveConfiguration(\Drupal::VERSION),
  ]);

  $drupalRoot = $drupalFinder->getDrupalRoot();
  $rectorConfig->autoloadPaths([
    $drupalRoot . '/core',
    $drupalRoot . '/modules',
    $drupalRoot . '/profiles',
    $drupalRoot . '/themes',
  ]);

  $rectorConfig->fileExtensions(
    ['php', 'module', 'theme', 'install', 'profile', 'inc', 'engine']
  );
  $rectorConfig->importNames(TRUE, FALSE);
  $rectorConfig->importShortClasses(FALSE);
};
