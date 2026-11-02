---
paths:
  - "agent/src/MergeRequest/**"
---

The sections render the run report into the MR/PR description. Each reads only what its report
section publishes, through `Read`. Keep them consistent with the existing style:

- Section headers: `## {emoji} {Title}`
- Table cells: always go through `Markdown::cell()` to escape pipe characters and newlines
- Wrap long sections in `<details>` / `<summary>` like the existing sections do
- Keep each section's golden in `agent/tests/MergeRequest/testdata/` byte for byte: the addon pages embed it
- Always verify output with `--dry-run` before shipping a new section
