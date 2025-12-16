# Contributing to blockchain-wallet-exporter

Thanks for taking the time to contribute!

This repository contains a Go-based Prometheus exporter plus a Helm chart under `charts/`.

## Development setup

### Prerequisites

- Go **1.23.x** (see `go.mod` and CI)
- `make`
- `docker` + `docker-compose` (for local run)
- `golangci-lint` (used by `make test-lint`)
- Helm 3.x (only needed if you touch the chart)

### Build

```bash
make build
```

### Run locally (docker-compose)

```bash
make run
```

On macOS (Apple Silicon), use:

```bash
make run-macos
```

Stop the stack:

```bash
make destroy
```

## Testing and linting

### Unit tests

```bash
make test
```

This runs unit tests (with coverage) for the main packages.

### Lint

```bash
make test-lint
```

## Contributing workflow

### 1) Create an issue (recommended)

- Use GitHub Issues for bugs, feature requests, and design discussions.
- For security-related concerns, please **do not** open a public issue; contact the maintainers.

### 2) Branch and implement

- Fork the repo and create a branch from `main`.
- Keep changes focused and scoped.
- Prefer small, reviewable PRs.

### 3) Keep code quality high

Before opening a PR, ensure:

- Code is formatted (`gofmt`) and idiomatic.
- `make test` passes.
- `make test-lint` passes.

### 4) Helm chart changes (if applicable)

If you modify anything in `charts/`:

- Lint the chart:

```bash
helm lint charts/*
```

- If you have chart-testing installed, you can run the same lint as CI:

```bash
ct lint --config .github/config/ct.yaml
```

- Bump chart version in `charts/blockchain-wallet-exporter/Chart.yaml` when changing templates/values.

### 5) Open a Pull Request

Include in your PR description:

- What changed and why
- Any configuration or migration notes
- How you tested (commands + outputs, if relevant)

PR checklist:

- [ ] `make test`
- [ ] `make test-lint`
- [ ] Helm lint (if chart changed)
- [ ] Documentation updated (if behavior/config changes)

## Contributor expectations

- Be respectful and constructive in issues and code reviews.
- Avoid committing secrets (wallet keys, API keys, faucet credentials). Use placeholders and documentation instead.

## License

By contributing, you agree that your contributions will be licensed under the repository’s license (Apache-2.0). See `LICENSE`.
