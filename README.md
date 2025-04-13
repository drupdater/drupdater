# Drupdater

Drupdater is a standalone tool for updating Drupal sites. It is designed to be run from the command line and can be used to update Drupal core and contributed modules.

## Features

- Update PHP dependencies
- Update configuration
- Update translations
- Fix PHP code style issues
- Remove deprecated code

# Usage

Drupdater can be integrated into your CI/CD pipeline or run manually.

## CI/CD Integration

### Gitlab CI

```yaml
drupdater_scheduled_job:
  image: 
    name: ghcr.io/drupdater/drupdater-php8.3:latest
    entrypoint: [""]
  script: 
    - /opt/drupdater/bin --repository $CI_PROJECT_URL --token <your_token>
  only:
    - schedules
```
