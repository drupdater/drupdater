# Use a private Composer registry

If the project depends on packages from Private Packagist, a self-hosted Satis, a GitLab
package registry or any other authenticated source, Composer needs credentials. Provide
them through **`COMPOSER_AUTH`**.

Drupdater does not consume the variable itself — it passes it through to the Composer
subprocess.

## The format

`COMPOSER_AUTH` is a JSON object, in the format documented by
[Composer](https://getcomposer.org/doc/03-cli.md#composer-auth).

=== "Private Packagist"

    ```json
    {"http-basic":{"repo.packagist.com":{"username":"token","password":"<your-token>"}}}
    ```

    The username is the literal word `token`; the password is your Private Packagist
    token.

=== "GitLab package registry"

    ```json
    {"gitlab-token":{"gitlab.example.com":"<your-token>"}}
    ```

=== "GitHub (private repos as sources)"

    ```json
    {"github-oauth":{"github.com":"<your-token>"}}
    ```

=== "Generic HTTP basic"

    ```json
    {"http-basic":{"satis.example.com":{"username":"<user>","password":"<password>"}}}
    ```

Several entries can be combined in one object.

## In CI

=== "GitHub Actions"

    Store the JSON as a repository secret named `COMPOSER_AUTH`, then:

    ```yaml
      - run: /opt/drupdater/bin
        env:
          DRUPDATER_TOKEN: ${{ secrets.DRUPDATER_TOKEN }}
          COMPOSER_AUTH: ${{ secrets.COMPOSER_AUTH }}
    ```

=== "GitLab CI"

    Store it as a **masked, protected** CI/CD variable named `COMPOSER_AUTH`. It is
    then already in the job environment — nothing further is needed:

    ```yaml
    drupdater:weekly:
      extends: .drupdater_base
      script:
        - /opt/drupdater/bin
    ```

    !!! note "Masking a JSON value"

        GitLab's masking rules reject values containing certain characters. If the JSON
        cannot be masked, store only the **token** as a masked variable and assemble the
        JSON in the job:

        ```yaml
          variables:
            COMPOSER_AUTH: '{"http-basic":{"repo.packagist.com":{"username":"token","password":"$PACKAGIST_TOKEN"}}}'
        ```

=== "Local docker run"

    ```bash
    docker run \
      -e COMPOSER_AUTH='{"http-basic":{"repo.packagist.com":{"username":"token","password":"<your-token>"}}}' \
      ghcr.io/drupdater/drupdater-php8.3:v0.12.0 \
      <token> --clone --repository-url <repository-url>
    ```

## Verify it

The `--full` [preflight check](run-preflight-checks.md) runs a real `composer install`,
which is exactly what fails when credentials are missing:

```bash
drupdater check --full
```

```text
✗ composer install
    Could not authenticate against repo.packagist.com
```

## How credentials are protected

Every string value inside the parsed JSON is registered individually with the log
redactor, so a token appears as `***` wherever Composer echoes it back — typically
embedded in a URL after a failed authenticated fetch.

Values keyed **`username`** are deliberately *not* redacted. Private Packagist's
documented form sets the username to the literal word `token`, and redacting that word
would mangle unrelated log output.

If the value does not parse as JSON, the whole raw string is registered as a fallback.

The same redactor is applied to the [run report](../reference/run-report.md), so
credentials cannot leak there either. See [Credentials and
redaction](../explanation/credentials-and-redaction.md).
