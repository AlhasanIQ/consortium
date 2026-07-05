# Contributor Notes

This repository is prepared for public development. Keep contributor guidance tool-neutral and avoid committing local assistant configuration, private planning notes, machine-specific paths, or secrets.

## Rules

- Do not commit `.env`, database files, logs, benchmark result artifacts, generated frontend bundles, or local scratch directories.
- Keep public docs focused on how to install, run, operate, and contribute to the project.
- Do not add commercial planning, sales material, private notes, or speculative roadmap commitments.
- Novomo integration is allowed in this repo, but detailed Novomo setup should link to the Novomo project.
- Benchloop is experimental; document sharp edges instead of hiding them.
- For local service management, POSIX shell tooling is an accepted v0.1 prerequisite.

## Verification

Before proposing publication, run the release audit and relevant build checks:

```bash
scripts/audit-release.sh
make test
make build-release
```
