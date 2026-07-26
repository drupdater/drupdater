# Explanation

Understanding-oriented discussion of how Drupdater is built and why. Nothing here is
required to use the tool — it is here so that when the tool does something surprising,
the behaviour makes sense rather than looking arbitrary.

<div class="grid cards" markdown>

-   **[How a run works](how-a-run-works.md)**

    ---

    The seven phases, what each one produces, and why "nothing to update" is a success
    rather than a failure.

-   **[The addon architecture](addon-architecture.md)**

    ---

    Why functionality is split into event subscribers, how addons steer the Composer
    update without calling each other, and why their relative priority matters.

-   **[The configuration model](configuration-model.md)**

    ---

    Why configuration is split between CLI flags and a committed file, and why the file
    is keyed by run type rather than by setting.

-   **[Why a run report](why-a-run-report.md)**

    ---

    Why a machine-readable report is written on every exit path, and why an addon that
    silently did nothing is the failure mode it exists to catch.

-   **[Credentials and redaction](credentials-and-redaction.md)**

    ---

    When a token is required and when it is not, how secrets are kept out of logs and
    reports, and why the redaction is value-based rather than pattern-based.

-   **[VCS provider detection](vcs-provider-detection.md)**

    ---

    How Drupdater decides whether a repository is GitHub or GitLab, and why GitLab is
    the fallback for every unrecognised host.

-   **[What Drupdater does not do](non-goals.md)**

    ---

    The deliberate omissions: it never deploys, and it never runs your test suite.

</div>
