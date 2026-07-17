# Contributing

Contributions are welcome. This project is early, so keep changes narrow, documented, and easy to review.

## Development Setup

```bash
cp .env.example .env
make frontend-install
make test
make dev
```

Set `OPENROUTER_API_KEY` only when you need real provider-backed runs. Most unit tests use mocks.

Local development scripts expect POSIX shell tools. Use Linux, macOS, or WSL.

## Checks

```bash
make test
make ci-frontend
make build-release
scripts/audit-release.sh
```

Run the smallest relevant check while iterating, then broader checks before submitting.

## Testing Standards

Tests should protect behavior and public contracts, not inflate coverage. Every test should fail for a plausible regression; remove tests that only execute code, duplicate a stronger case, mirror the implementation, or assert only that a call returned no error.

Keep tests deterministic, isolated, and fast. Prefer fakes and controlled synchronization over live services, global state, wall-clock sleeps, and polling. Cover high-risk boundaries and negative paths such as malformed input, cancellation, retries, idempotency, persistence, ordering, and partial failure. Use table-driven tests when the behavior matrix is the point, while keeping fixtures and assertions specific enough to explain the contract.

## Documentation

Public docs should be concise and operational. Avoid business strategy, sales language, private planning history, and local machine paths.

## Security

Do not include secrets, prompt/output dumps, local databases, benchmark datasets, or generated run artifacts in commits. See [SECURITY.md](SECURITY.md).
