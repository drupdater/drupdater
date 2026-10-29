---
paths:
  - "internal/mergerequest/templates/**"
---

Templates render the run report into the MR/PR description. Each reads only what its report
section publishes, via `sections` in `internal/mergerequest/mergerequest.go`. Keep them consistent with the existing style:

- Section headers: `## {emoji} **{Title}**`
- Table cells: always use the `{{ cell }}` helper to escape pipe characters
- Wrap long sections in `<details>` / `<summary>` like the existing templates do
- Always verify output with `--dry-run` before shipping a new template
