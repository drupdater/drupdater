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
