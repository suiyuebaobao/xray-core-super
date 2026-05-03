# Security Policy

RayPilot manages subscription links, node agents, relay agents, traffic accounting, and deployment workflows. Treat all deployment data as sensitive.

## Reporting a Vulnerability

Please do not open a public issue with exploitable details, subscription tokens, node tokens, private keys, server credentials, database dumps, or customer data.

Preferred reporting paths:

- Use GitHub private vulnerability reporting if it is enabled for this repository.
- Otherwise contact the maintainer through the contact channels listed in `README.md`.

Include:

- Affected commit or deployment version
- Reproduction steps
- Impact and affected component
- Minimal logs or screenshots with all secrets removed

## Sensitive Data

Never publish:

- `.env` files
- JWT secrets
- subscription tokens
- node-agent or relay-agent tokens
- Reality private keys
- SSH usernames/passwords or private keys
- real customer data
- production database dumps

## Supported Version

The `main` branch is the active development line. Security fixes are applied there first unless release branches are introduced later.
