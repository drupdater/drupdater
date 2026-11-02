<?php

declare(strict_types=1);

use Drupdater\App\Json;

require __DIR__ . '/../../vendor/autoload.php';

// Stands in for composer: answers as the spec file in argv[1] says, and records what it was asked.
$spec = json_decode((string) file_get_contents($argv[1]), false, 512, JSON_THROW_ON_ERROR);
file_put_contents($argv[1] . '.seen', json_encode(['argv' => array_slice($argv, 2), 'stdin' => stream_get_contents(STDIN), 'cwd' => getcwd()]));
fwrite(STDOUT, Json::string(Json::at($spec, 'stdout')));
fwrite(STDERR, Json::string(Json::at($spec, 'stderr')));
exit(Json::int(Json::at($spec, 'exit')));
