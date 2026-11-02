This automated merge request by [Drupdater](https://github.com/drupdater/drupdater) includes updates for your Drupal site. Please review the changes carefully to ensure compatibility and stability before merging.

## 🔌 New Composer plugins

During the update process, new Composer plugins were detected and added to the allowlist with their initial value set to false:

- php-http/discovery
- tbachert/spi

Please review the changes and adjust the values as needed. Run the following command to allow the plugins:

```bash
composer config allow-plugins.php-http/discovery true
composer config allow-plugins.tbachert/spi true
```

## 🩹 Patch updates

✅ **Removed Patches**

The following patches were removed as they are no longer needed:

- Fix the thing:

  - Package: drupal/core
  - Patch: `https://www.drupal.org/files/issues/3001-12.patch`
  - Reason: fixed upstream in 10.4.0

🔄 **Updated Patches**

- Support the other thing

  - Package: drupal/paragraphs
  - Previous patch: `https://www.drupal.org/files/issues/3002-4.patch`
  - New patch: `https://www.drupal.org/files/issues/3002-9.patch`

⚠️ **Packages Not Updated Due to Patch Conflicts**

- drupal/webform was kept at version 6.2.7 because

  - Description: Adjust the form
  - Patch: `https://www.drupal.org/files/issues/3003-2.patch`
  - Reason: Patch is not compatible with the new version 6.3.0 and a newer patch that is compatible with the new version is not available

## 🛠️ Dependency updates

<details open>
<summary>Open/close</summary>

| Prod Packages | Operation | Base | Target | Link |
| --- | --- | --- | --- | --- |
| [drupal/core](https://github.com/drupal/core) | Upgraded | 10.3.8 | 10.3.9 | [Compare](https://github.com/drupal/core/compare/10.3.8...10.3.9) |
| [drupal/redirect](https://www.drupal.org/project/redirect) | New | - | 1.10.0 |  |
| drupal/legacy_feature | Removed | 1.2.0 | - |  |

| Dev Packages | Operation | Base | Target | Link |
| --- | --- | --- | --- | --- |
| phpunit/phpunit | Upgraded | 10.5.30 | 10.5.31 | [Compare](https://github.com/sebastianbergmann/phpunit/compare/10.5.30...10.5.31) |

</details>

## 🛡️ Security Report

The security report shows fixed and unfixed security vulnerabilities. There are still 1 unresolved issue. Please investigate manually.

| Status | CVE      | Title | Severity | Package  |
| ------ | -------- | ----- | -------- | -------- |
| ✅     | CVE-2026-1111 | Cross site scripting in the render pipeline | critical | drupal/core |
| ⛔     | CVE-2026-2222 | Access bypass | moderate | drupal/token |

## ⚠️ Unsupported modules and abandoned packages

The following are no longer maintained: an unsupported module has no supported release on Drupal.org, an abandoned package has been marked as such by its maintainers. Neither will receive further fixes, including security ones, and no update will resolve it. Each needs a decision — replace it, adopt it, or accept the risk.

| Name | Status | Installed version | Recommendation |
| ---- | ------ | ----------------- | -------------- |
| legacy_feature | Unsupported module | 1.2.0 | No supported release — replace it |
| old_widget | Unsupported module | 2.0.0 | Update to 3.1.0 |
| patchwork/jsqueeze | Abandoned package | — | No replacement suggested |
| swiftmailer/swiftmailer | Abandoned package | — | Replace with symfony/mailer |

## 📄 Job Logs

<details>
<summary>⚙️ Update Hooks</summary>

### Site: default

| Hook | Description |
| ---- | ----------- |
| token_update_8002 | Rebuild the token cache |

### Site: second

| Hook | Description |
| ---- | ----------- |
| node_post_update_index | Reindex nodes |

</details>
