# HTTP API

All endpoints return JSON. Except for login, API routes require a valid
`palctrl_session` cookie. Mutation routes also require:

```http
X-Pal-Control: 1
Content-Type: application/json
```

The API is intended for the bundled same-origin interface. It is not a stable
third-party public API.

## Authentication

### `POST /api/login`

Request:

```json
{
  "password": "panel-password"
}
```

Success:

```json
{
  "ok": true
}
```

Sets the HttpOnly session cookie.

### `POST /api/logout`

Request:

```json
{}
```

Clears the session cookie.

## State

### `GET /api/state`

Returns:

- Online state
- Container or process status
- Control mode
- Server information
- Palworld metrics
- Connected players
- Effective settings summary
- Host CPU, memory, network and block I/O
- Current integration issues
- Enabled feature flags

### `GET /api/history`

Returns the in-memory 24-hour sample history:

```json
{
  "samples": [
    {
      "at": "2026-01-01T00:00:00Z",
      "fps": 59.5,
      "frameTime": 16.8,
      "players": 2,
      "memoryUsage": "3.2GiB / 7GiB"
    }
  ]
}
```

### `GET /api/logs`

Returns recent Docker or native process log lines.

### `GET /api/backups`

Returns ZIP backup metadata sorted newest first.

## Actions

### `POST /api/action`

Base request:

```json
{
  "action": "save"
}
```

Supported actions:

| Action | Additional fields | Behavior |
| --- | --- | --- |
| `start` | None | Starts the server and waits for readiness |
| `restart` | None | Restarts and waits for readiness |
| `update` | None | Pulls and recreates the configured Compose service |
| `save` | None | Requests an in-game world save |
| `shutdown` | `message`, `waitTime` | Warns players, saves and stops gracefully |
| `force-stop` | None | Stops without the in-game shutdown path |
| `announce` | `message` | Sends a player announcement |
| `kick` | `userId`, optional `message` | Removes a player |
| `ban` | `userId`, optional `message` | Bans a player |
| `backup` | None | Saves, waits briefly and creates a ZIP |

Success:

```json
{
  "ok": true,
  "message": "World saved"
}
```

## Console

### `POST /api/console`

Request:

```json
{
  "command": "Info"
}
```

Success:

```json
{
  "ok": true,
  "output": "command response"
}
```

The console uses deprecated RCON. The command is limited to 500 characters.

## Game settings

### `GET /api/game-settings`

Returns:

- Eight setting groups
- 101 definitions
- Six presets
- Current effective values
- Editability
- Rollback availability
- Value source

Definitions include type, bounds, unit, options, description and official
default.

### `POST /api/game-settings/apply`

Apply a preset:

```json
{
  "preset": "boss-assist",
  "changes": {}
}
```

Apply custom settings:

```json
{
  "preset": "",
  "changes": {
    "ExpRate": 2.5,
    "PalCaptureRate": 1.5,
    "DeathPenalty": "None"
  }
}
```

A request must use either `preset` or `changes`, not both.

The operation can take more than one minute because it saves, backs up,
restarts and polls the Palworld REST health endpoint.

Success:

```json
{
  "ok": true,
  "message": "Custom settings applied; Palworld is ready",
  "backup": {
    "name": "palworld-20260101-000000.zip",
    "size": 123456,
    "createdAt": "2026-01-01T00:00:00Z"
  }
}
```

### `POST /api/game-settings/rollback`

Request:

```json
{}
```

Restores the latest settings snapshot through the full safety workflow.

## Errors

Errors use:

```json
{
  "error": "human-readable message"
}
```

Common status codes:

| Status | Meaning |
| --- | --- |
| `400` | Invalid JSON, unsupported setting or invalid value |
| `401` | Missing or invalid session |
| `403` | Missing mutation header |
| `405` | Wrong HTTP method |
| `412` | Required integration is not configured |
| `502` | Palworld, Docker, RCON or restart workflow failed |

Request bodies are limited to 32 KiB and unknown JSON fields are rejected.

