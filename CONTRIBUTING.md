# Contributing to Helixon Autoresearch

## Development

```bash
git clone https://github.com/nfsarch33/helixon-autoresearch.git
cd helixon-autoresearch
go test -race ./...
go vet ./...
```

## Quality Gates

- 70%+ test coverage on all packages
- `go test -race` must pass
- `go vet` must pass
- `golangci-lint run` must pass
- No credential leaks (gitleaks scan)

## Pull Requests

- One feature per PR
- Tests required for new code
- Conventional commit messages
