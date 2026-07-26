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

// The procedural UPDATE_* constants were deprecated in Drupal 10.3 and removed
// in 11, where referencing one is a fatal "Undefined constant" rather than a
// notice. The addon logs and swallows that failure, so the only symptom is that
// no unsupported module is ever reported.
$notSupported = \Drupal\update\UpdateManagerInterface::NOT_SUPPORTED;

$unsupported = [];
foreach ($data as $name => $project) {
    if (($project['status'] ?? NULL) !== $notSupported) {
        continue;
    }
    $unsupported[] = [
        'name' => $name,
        'installed_version' => $project['existing_version'] ?? 'unknown',
        'recommended_version' => $project['recommended'] ?? 'None',
    ];
}

echo json_encode($unsupported);
