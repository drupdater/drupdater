<?php

declare(strict_types=1);

// Rector includes this with PHPStan's container as $container, as PHPStan would phpstan-drupal's
// own drupal-autoloader.php. A project phpstan-drupal cannot load keeps its deprecation fixes:
// only the types of \Drupal::service() calls are lost.

use mglaman\PHPStanDrupal\Drupal\DrupalAutoloader;

if (!defined('DRUPAL_TEST_IN_CHILD_SITE')) {
  define('DRUPAL_TEST_IN_CHILD_SITE', FALSE);
}

try {
  (new DrupalAutoloader())->register($container);
}
catch (\Throwable $e) {
  fwrite(STDERR, 'phpstan-drupal could not load the project, so \Drupal::service() calls stay untyped: ' . $e->getMessage() . PHP_EOL);
}
