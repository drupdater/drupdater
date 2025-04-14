# Drupdater

Drupdater is a standalone tool for updating Drupal sites. It is designed to streamline the process of updating Drupal core, contributed modules, and configurations. It also provides tools for fixing PHP code style issues and removing deprecated code.

## Features

- Update PHP dependencies using Composer.
- Remove/update merged/updated patches.
- Export and update Drupal configurations.
- Update translations for multilingual sites.
- Fix PHP code style issues using `phpcs` and `phpcbf`.
- Remove deprecated code using `drupal-rector`.
- Multisite support for updating multiple Drupal sites in one merge request.

## Installation

Drupdater is distributed as a Docker image. To use it, ensure you have Docker installed on your system.

### Pull the Docker Image

```bash
docker pull ghcr.io/drupdater/drupdater-php8.3:latest
```

## Usage

Drupdater can be integrated into your CI/CD pipeline or run manually from the command line.

### Command-Line Usage

Run the following command to update a Drupal site:

```bash
docker run --rm ghcr.io/drupdater/drupdater-php8.3:latest <repository_url> <your_token>
```

Replace `<repository_url>` with the URL of your Drupal site's repository and `<your_token>` with your authentication token.

### CI/CD Integration

#### GitLab CI

Integrate Drupdater into your GitLab CI pipeline using the following configuration:

```yaml
drupdater_scheduled_job:
  image: 
    name: ghcr.io/drupdater/drupdater-php8.3:latest
    entrypoint: [""]
  script: 
    - /opt/drupdater/bin $CI_PROJECT_URL <your_token>
  only:
    - schedules
```

## Configuration

Drupdater supports the following flags:

- `--branch`: The branch to update (default: `main`).
- `--sites`: A list of Drupal site directories to update (default: `default`).
- `--update-strategy`: Update strategy (`Regular` or `Security`).
- `--auto-merge`: Automatically merge the update branch if set to `true`.
- `--skip-cbf`: Skip running `phpcbf` for fixing code style issues.
- `--skip-rector`: Skip running `drupal-rector` for removing deprecated code.
- `--dry-run`: Perform a dry run without creating branches or merge requests.
- `--verbose`: Enable verbose logging.
