# Development

## Build mocks

To build mocks, you need `mockery`:

```bash
docker run -v "$PWD":/src -w /src -e GOFLAGS="-buildvcs=false" vektra/mockery:v2
```

## Execute tests

```bash
go test -v ./...
```

## Execute programm

```bash
go run cmd/updater/main.go "{
  \"repositoryUrl\": \"https://gitlab.com/my-project\",
  \"branch\": \"main\",
  \"token\": \"token\",
  \"sites\": [
    \"default\"
  ],
  \"updateStrategy\": \"Regular\",
  \"autoMerge\": true,
  \"runCBF\": true,
  \"runRector\": false,
  \"dryRun\": false,
  \"verbose\": true
}"
```
