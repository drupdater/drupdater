---
paths:
  - "**/*.go"
  - "Dockerfile"
  - ".golangci.yml"
---

After writing or modifying Go code, the Dockerfile, or lint configuration, run:

```bash
make lint
```

Work is only complete when `make lint` reports zero issues.
