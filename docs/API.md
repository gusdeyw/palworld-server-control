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
- Rolling network latency, packet loss, status and per-target probe results
- Current integration issues
- Enabled feature flags

The `network` object uses `collecting`, `healthy`, `degraded`, `critical` or
`disabled` status. Packet loss and latency are calculated over a rolling probe
window:

```json
{
  "network": {
    "enabled": true,
    "status": "healthy",
    "latencyMs": 35.2,
    "packetLoss": 0,
    "sent": 40,
    "received": 40,
    "windowSize": 40,
    "updatedAt": "2026-01-01T00:00:00Z",
    "targets": [
      {
        "target": "1.1.1.1:53",
        "latencyMs": 34.1,
        "packetLoss": 0,
        "sent": 20,
        "received": 20
      }
    ]
  }
}
```

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
      "memoryUsage": "3.2GiB / 7GiB",
      "latencyMs": 35.2,
      "packetLoss": 0
    }
  ]
}
```

## Reports

### `GET /api/reports`

Returns daily report summaries newest first, along with the calendar timezone
and retention policy:

```json
{
  "timezone": "Asia/Makassar",
  "retentionDays": 30,
  "reports": [
    {
      "date": "2026-07-27",
      "status": "degraded",
      "samples": 1440,
      "gameSamples": 1439,
      "networkSamples": 1440,
      "onlinePercent": 99.9,
      "averageFps": 59.1,
      "minimumFps": 52,
      "peakPlayers": 4,
      "averageLatencyMs": 3.2,
      "maximumLatencyMs": 38.4,
      "averagePacketLoss": 0.3,
      "maximumPacketLoss": 5,
      "degradedSamples": 2,
      "criticalSamples": 0,
      "size": 184320
    }
  ]
}
```

The daily status is the worst recorded network classification for that day.
API availability is the percentage of samples where the Palworld REST API
responded.

### `GET /api/reports/{YYYY-MM-DD}`

Returns the selected daily summary and compact hourly buckets. Each hourly
bucket includes API availability, average FPS, peak players, average latency,
maximum loss and the worst network state.

### `GET /api/reports/{YYYY-MM-DD}/download`

Downloads the original human-readable CSV as
`palctrl-report-YYYY-MM-DD.csv`. The download requires the normal authenticated
session cookie.

### `GET /api/logs`

Returns recent Docker or native process log lines.

### `GET /api/backups`

Returns ZIP backup metadata sorted newest first, plus the total count and size:

```json
{
  "count": 2,
  "totalSize": 391245824,
  "backups": [
    {
      "name": "palworld-20260727-062532.zip",
      "size": 195622912,
      "createdAt": "2026-07-27T06:25:32Z"
    }
  ]
}
```

### `DELETE /api/backups/{name}`

Permanently deletes one regular `.zip` file from the configured backup
directory. The filename is validated and path traversal, directories, symbolic
links and non-ZIP files are rejected. This mutation requires `X-Pal-Control: 1`.

### `GET /api/backups/{name}/download`

Downloads the selected ZIP backup using the authenticated session cookie.
The filename receives the same traversal and regular-file validation as
deletion. Byte-range requests are supported so interrupted large downloads can
resume.

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
| `update` | None | Saves and backs up the world, pulls and force-recreates the configured Compose service, verifies the image ID, starts the server, and reports the running game version |
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

- Nine setting groups
- 102 definitions
- Six presets
- Current effective values
- Editability
- Rollback availability
- Value source

Definitions include type, bounds, unit, options, description and official
default.

The Server group includes `ServerName` (required, maximum 64 characters) and
`ServerPlayerMaxNum` (1–32). When the live REST response omits these identity
values, PAL CTRL reads their authoritative values from `PalWorldSettings.ini`.

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
