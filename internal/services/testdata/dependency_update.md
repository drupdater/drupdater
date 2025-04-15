This automated merge request includes updates for your Drupal site. Please review the changes carefully to ensure compatibility and stability before merging.

## 🩹 Patch updates

✅ **Removed Patches**

The following patches were removed as they are no longer needed:

- package1 not installed anymore:

  - Package: package1
  - Patch: `patch1`
  - Reason: reason1

- Issue #issue1: [title1](link1) was fixed in version 2.0:

  - Package: package1
  - Patch: `patch1`
  - Reason: Fixed

🔄 **Updated Patches**

- description

  - Package: package2
  - Previous patch: `oldPatch`
  - New patch: `newPatch`

⚠️ **Packages Not Updated Due to Patch Conflicts**

- package3 was fixed to version 2.0 because

  - Description: description
  - Patch: `patch3`
  - Reason: Patch is not compatible with the new version 3.0 and a newer patch that is compatible with the new version is not available

## 🔌 New Composer plugins

During the update process, new Composer plugins were detected and added to the allowlist with their initial value set to false:

- plugin1
- plugin2

Please review the changes and adjust the values as needed. Run the following command to allow the plugins:

```bash
composer config allow-plugins.plugin1 true
composer config allow-plugins.plugin2 true
```

## 🛠️ Dependency updates

<details open>
<summary>Open/close</summary>

Dummy Table

</details>

## 📄 Job Logs

<details>
<summary>⚙️ Update Hooks</summary>

| Hook | Description |
| ---- | ----------- |
| hook | description |

</details>

