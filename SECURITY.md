# Security

## Supported Status

Consortium v0.1 is suitable for local development and trusted operator deployments. It is not yet hardened as a multi-tenant public SaaS boundary.

## Reporting

Please report vulnerabilities privately through the repository security advisory flow once available, or by contacting the maintainer listed on the GitHub repository.

## v0.1 Deployment Guidance

- Keep the backend on loopback unless it is behind TLS and trusted authentication.
- Set `ADMIN_API_TOKEN` before exposing `/api/admin/*`.
- Treat `/api/workflows`, `/api/jobs`, and the admin UI as operator/local UI surfaces.
- Expose `/v1/*` to clients only after creating Consortium API keys and configuring request/token limits.
- Store SQLite databases and logs in a private directory.
- Do not publish raw working directories; use release artifacts or Git archives.
- Review Novomo network boundaries before using `agent_run` or `novo_run` nodes.

## Sensitive Data

Workflow jobs can contain prompts, responses, model metadata, costs, traces, and config snapshots. Logs can contain upstream errors. Back up and prune these files according to your own data retention requirements.
