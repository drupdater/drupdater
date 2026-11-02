<?php

declare(strict_types=1);

use Drupdater\App\Git\Git;
use Drupdater\App\Git\GitCommitter;
use Drupdater\App\Git\Identity;
use Drupdater\App\Json;

require __DIR__ . '/../../vendor/autoload.php';

[, $dir, $message, $path] = Json::strings($_SERVER['argv'] ?? []);
$made = (new GitCommitter(new Git($dir), new Identity('n', 'e@e')))->commit($message, [$path]);
echo json_encode($made), "\n";
