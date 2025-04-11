<?php
declare(strict_types=1);

$factory = \Drupal::configFactory();
foreach ($factory->listAll() as $name) {
    $factory->getEditable($name)->save();
}
