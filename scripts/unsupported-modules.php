<?php
declare(strict_types=1);

if (!\Drupal::moduleHandler()->moduleExists('update')) {
    echo json_encode([]);
    return;
}

\Drupal::moduleHandler()->loadInclude('update', 'inc', 'update.compare');
\Drupal::moduleHandler()->loadInclude('update', 'inc', 'update.fetch');

$available = update_get_available(TRUE);
$data = update_calculate_project_data($available);

$unsupported = [];
foreach ($data as $name => $project) {
    if (($project['status'] ?? NULL) !== UPDATE_NOT_SUPPORTED) {
        continue;
    }
    $unsupported[] = [
        'name' => $name,
        'installed_version' => $project['existing_version'] ?? 'unknown',
        'recommended_version' => $project['recommended'] ?? 'None',
    ];
}

echo json_encode($unsupported);
