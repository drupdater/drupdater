## 🔌 New Composer plugins

During the update process, new Composer plugins were detected and added to the allowlist with their initial value set to false:

- php-http/discovery
- tbachert/spi

Please review the changes and adjust the values as needed. Run the following command to allow the plugins:

```bash
composer config allow-plugins.php-http/discovery true
composer config allow-plugins.tbachert/spi true
```
