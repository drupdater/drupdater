# Drupdater

Drupdater is a standalone tool for updating Drupal sites. It is designed to streamline the process of updating Drupal core, contributed modules, and configurations. It also provides tools for fixing PHP code style issues and removing deprecated code. After completing updates, Drupdater automatically creates a merge request (GitLab) or pull request (GitHub) with detailed changelogs and security-related updates highlighted.

## Table of Contents
- [Features](#features)
- [Prerequisites](#prerequisites)
- [Usage](#usage)
  - [CI/CD Integration](#cicd-integration)
- [Configuration](#configuration)
  - [Environment Variables](#environment-variables)

## Features

- Update PHP dependencies using Composer.
- Security scans for vulnerable dependencies.
- Remove/update merged/updated patches.
- Export and update Drupal configurations.
- Update translations for multilingual sites.
- Fix PHP code style issues using `phpcs` and `phpcbf`.
- Remove deprecated code using `drupal-rector`.
- Detects new composer plugins and adds them to the `composer.json` file.
- Runs `composer normalize` if available to ensure consistent formatting of the `composer.json` file.
- Multisite support for updating multiple Drupal sites in one merge request.
- Automatic creation of merge/pull requests with detailed changelogs for both GitLab and GitHub.

## Prerequisites
- Your Drupal site must be installable from configuration.
- Your Drupal site uses GitLab/GitHub for version control.
- A Drupal.org GitLab access token for patch removal.

## Usage

Drupdater can be integrated into your CI/CD pipeline or run manually from the command line.

### Command-Line Usage

Run the following command to update a Drupal site:

```bash
docker run ghcr.io/drupdater/drupdater-php8.3:latest <repository_url> <your_token>
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
- `--security`: Only update vulnerable dependencies.
- `--auto-merge`: Automatically merge the update branch if set to `true`.
- `--skip-cbf`: Skip running `phpcbf` for fixing code style issues.
- `--skip-rector`: Skip running `drupal-rector` for removing deprecated code.
- `--dry-run`: Perform a dry run without creating branches or merge requests.
- `--verbose`: Enable verbose logging.

### Environment Variables

- `DRUPALCODE_ACCESS_TOKEN`: Set this to your Drupal.org GitLab access token to enable patch removal of committed patches during updates.

## FAQ

### I am using a private packagist. How can Drupdater access it?

Drupdater uses the `COMPOSER_AUTH` environment variable to access private packagist repositories. You can set this variable in your CI/CD pipeline or pass it as an argument when running Drupdater. See the [Composer documentation](https://getcomposer.org/doc/03-cli.md#composer-auth) for more information on how to set up authentication for private repositories.
