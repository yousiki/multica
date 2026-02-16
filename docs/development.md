# Development Guide

## Prerequisites

- Node.js 20+
- pnpm 10+
- macOS/Linux/Windows

## Install

```bash
pnpm install
```

`.npmrc` must keep:

```ini
shamefully-hoist=true
```

## Main Dev Entry Points

```bash
# Recommended local desktop workflow
pnpm dev

# Service-specific
pnpm dev:desktop
pnpm dev:gateway
pnpm dev:web

# Full local stack with isolated dev data
pnpm dev:local
pnpm dev:local:archive
```

## What Each Command Does

- `pnpm dev`: builds shared packages, then runs `types + utils + core + desktop` watch flow.
- `pnpm dev:desktop`: Electron desktop only.
- `pnpm dev:gateway`: NestJS WebSocket gateway (`PORT`, default `3000`).
- `pnpm dev:web`: Next.js web app (`3000` by script).
- `pnpm dev:local`: gateway + web + desktop with dev-safe env defaults.
- `pnpm dev:local:archive`: archive dev data and start fresh.

## Important Environment Variables

- `SMC_DATA_DIR`: override runtime data root (default `~/.super-multica`)
- `GATEWAY_URL`: gateway endpoint for desktop/CLI hub connection
- `MULTICA_API_URL`: required by web/data tools
- `PORT`: gateway/server port
- `MULTICA_WORKSPACE_DIR`: override workspace root
- `MULTICA_RUN_LOG=1`: enable structured run-log output

## Agent / Conversation Semantics

- `agentId`: logical owner identity (capabilities/profile scope).
- `conversationId`: isolated runtime thread under an agent.
- `sessionId`: runtime/storage id for a conversation (currently same as `conversationId`).

Compatibility behavior:

- If only `agentId` is provided, runtime resolves to that agent's `mainConversationId`.
- Legacy fallback is still supported: when no mapping exists, `conversationId = agentId`.
- New integrations should pass `conversationId` explicitly.
- Hub RPC supports both naming sets:
  - Legacy: `createAgent/listAgents/deleteAgent`
  - Conversation-first aliases: `createConversation/listConversations/deleteConversation`
  - `createConversation` supports optional `agentId` to create a new thread under a specific agent.

Telegram behavior:

- One Telegram DM binds to one active `conversationId`.
- `/new` creates and switches to a new conversation.
- `/session <id>` switches the active conversation.
- `/sessions` lists available conversations.

## Local Full-Stack Notes (`pnpm dev:local`)

`pnpm dev:local` is the recommended way to run the full local stack for integration work.

Setup:

1. `cp .env.example .env`
2. Set `TELEGRAM_BOT_TOKEN` in root `.env`
3. Run `pnpm dev:local`

Services started by the script:

| Service | Address | Notes |
|---------|---------|-------|
| Gateway | `http://localhost:4000` | Telegram long-polling mode (`PORT=4000`) |
| Web | `http://localhost:3000` | OAuth login / frontend |
| Desktop | — | Uses `GATEWAY_URL=http://localhost:4000` and local web URL |

Data/workspace isolation used by the script:

- `SMC_DATA_DIR=~/.super-multica-dev`
- `MULTICA_WORKSPACE_DIR=~/Documents/Multica-dev`

Why this matters:

- avoids polluting production data under `~/.super-multica`
- provides a stable local target for auth/session debugging

Common follow-up:

```bash
pnpm dev:local:archive
```

This archives prior dev data before starting fresh local runs.

## Build / Quality

```bash
pnpm build
pnpm typecheck
pnpm test
pnpm test:coverage
```

## Useful Reset Commands

```bash
# Reset default + dev data dirs used by desktop scripts
pnpm dev:desktop:reset

# Reset and relaunch desktop onboarding flow
pnpm dev:desktop:fresh
pnpm dev:desktop:onboarding
```
