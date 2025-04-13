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
go run main.go <repository> <token> 
```
