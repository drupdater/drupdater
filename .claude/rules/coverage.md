---
paths:
  - "**/*.go"
---

After writing or modifying Go code, verify coverage of the changed package before considering the task done:

```bash
go test -coverprofile=coverage.out ./path/to/changed/package/...
go tool cover -func=coverage.out
```

Work is only complete when the changed packages reach **≥ 90% coverage**. If below 90%, add tests first.
