# Contributing to RayPilot

RayPilot is a Go + Vue project for Xray/VLESS Reality subscription management, relay nodes, node-agent sync, and traffic accounting.

## Local Checks

Before opening a pull request, run:

```bash
go test ./...
cd frontend && npm run build
docker-compose config --services
```

If your environment uses Compose v2 only, run:

```bash
COMPOSE="docker compose" make up
```

## Development Rules

- Keep SQL table changes in `migrations/`; do not rely on GORM auto-migration.
- Keep handler code focused on request parsing and responses.
- Put business rules in `internal/service` and persistence behavior in `internal/repository`.
- Use `frontend/src/api/request.js` or API adapter modules for frontend API calls.
- Add focused tests for auth, subscription tokens, traffic accounting, node sync, relay behavior, and billing-related logic.
- Do not commit `.env`, secrets, generated binaries, coverage files, logs, `frontend/node_modules`, or frontend build output.

## Screenshots

README screenshots are sanitized and generated with mocked API data:

```bash
cd frontend && npm run screenshots
```

Do not publish screenshots containing real server IPs, subscription tokens, node tokens, private keys, or customer data.
