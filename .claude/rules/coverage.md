---
paths:
  - "**/*.go"
---

Changed packages must reach **≥ 90% coverage**. This is checked automatically on `git commit` (pre-commit hook prints per-package totals) — read that output and add tests before committing if any package is below 90%.
