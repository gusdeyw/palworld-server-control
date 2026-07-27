# Configuration reference

PAL CTRL is configured entirely through environment variables.

## Panel

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `PANEL_ADDR` | No | `127.0.0.1:8080` | HTTP listen address |
| `PANEL_PASSWORD` | Yes | None | Shared panel password; startup fails when empty |
| `PANEL_SECURE_COOKIES` | No | `false` | Set `true` when HTTPS terminates directly in front of PAL CTRL |
| `PALWORLD_MOCK` | No | `false` | Enables sample mode without real server mutations |

## Palworld APIs

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `PALWORLD_REST_URL` | Production | `http://127.0.0.1:8212` | Private Palworld REST base URL |
| `PALWORLD_ADMIN_USER` | No | `admin` | REST Basic Auth user |
| `PALWORLD_ADMIN_PASSWORD` | Production | None | REST administrative password |
| `PALWORLD_RCON_ADDR` | Legacy console | `127.0.0.1:25575` | Private RCON address |
| `PALWORLD_RCON_PASSWORD` | Legacy console | Admin password | RCON password |

Pocketpair has deprecated RCON and plans to remove it in a future release.
Lifecycle, player management, saves and announcements use REST whenever
possible. Keep the console clearly identified as legacy functionality.

## Lifecycle control

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `PALWORLD_CONTAINER` | Docker | `palworld` | Managed container name |
| `PALWORLD_COMPOSE_DIR` | Updates | Empty | Compose project directory |
| `PALWORLD_COMPOSE_SERVICE` | No | `palworld` | Compose service used for updates |
| `PALWORLD_CONTROL_URL` | Native Windows | Empty | Native bridge base URL |
| `PALWORLD_CONTROL_TOKEN` | Native Windows | Empty | Shared bridge token |

If `PALWORLD_CONTROL_URL` is set, the panel uses the Windows bridge. Otherwise
it uses Docker.

## Storage

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `PALWORLD_SAVE_DIR` | Backups/settings | Empty | Read-only save directory visible inside PAL CTRL |
| `PALWORLD_BACKUP_DIR` | Yes | `./backups` | Writable ZIP backup directory |
| `PALWORLD_SETTINGS_PATH` | Settings editor | Empty | Writable `PalWorldSettings.ini` path |
| `PALWORLD_SETTINGS_STATE_DIR` | Settings editor | `./backups/settings` | Normal baseline and INI snapshot directory |

Recommended container paths:

```dotenv
PALWORLD_SAVE_DIR=/palworld-saved
PALWORLD_BACKUP_DIR=/backups
PALWORLD_SETTINGS_PATH=/palworld-config/PalWorldSettings.ini
PALWORLD_SETTINGS_STATE_DIR=/backups/settings
```

Recommended mounts:

```yaml
volumes:
  - /opt/palworld/data/Saved:/palworld-saved:ro
  - /opt/palworld/data/Saved/Config/LinuxServer:/palworld-config
  - /opt/palworld/data/backups:/backups
```

The broad save mount remains read-only. Only the configuration directory and
backup destination are writable.

## Timing

| Variable | Default | Description |
| --- | --- | --- |
| `METRICS_INTERVAL` | `30s` | Sampling interval, minimum five seconds |
| `PALWORLD_SHUTDOWN_WAIT` | `15` | Default graceful shutdown delay |
| `PALWORLD_SHUTDOWN_MESSAGE` | Friendly default | Player-facing shutdown message |

## Network monitoring

PAL CTRL sends low-rate UDP DNS probes directly to independent external
resolvers. This does not require raw ICMP privileges or additional container
capabilities. Results are aggregated into a rolling latency and packet-loss
window and exposed in the state API and dashboard issue banner.

| Variable | Default | Description |
| --- | --- | --- |
| `NETWORK_PROBE_TARGETS` | `1.1.1.1:53,8.8.8.8:53` | Comma-separated UDP DNS probe targets; empty disables monitoring |
| `NETWORK_PROBE_INTERVAL` | `5s` | Delay between probe cycles, minimum one second |
| `NETWORK_PROBE_TIMEOUT` | `2s` | Per-target response timeout, minimum 100 milliseconds |
| `NETWORK_PROBE_WINDOW` | `20` | Rolling samples retained per target, minimum four |
| `NETWORK_DEGRADED_LOSS` | `5` | Packet-loss percentage that raises a degraded warning |
| `NETWORK_CRITICAL_LOSS` | `20` | Packet-loss percentage that raises a critical warning |

With two default targets and a window of 20, the dashboard summarizes the most
recent 40 probes. Monitoring starts warning only after at least ten total
samples have been collected, or after the complete window when configured
smaller.

These probes measure the VPS's outbound UDP path and the return path from the
configured targets. They cannot replace an external monitor testing the
player-facing route into the VPS.

## Palworld requirements

The Palworld INI must enable:

```ini
RESTAPIEnabled=True
RESTAPIPort=8212
RCONEnabled=True
RCONPort=25575
bIsUseBackupSaveData=True
```

RCON is optional if the legacy console is not needed.

## Game Night catalog

The settings page groups 101 official controls into:

- Combat
- Progression
- Survival
- World
- Resources
- Bases
- Players
- Performance

The browser cannot edit infrastructure fields such as:

- `AdminPassword`
- `ServerPassword`
- `PublicIP`
- `PublicPort`
- REST and RCON ports
- Authentication and ban-list URLs

This separation prevents a Game Night preset from disabling management access
or changing connection credentials.

## Password handling

Use different passwords for:

1. VPS/SSH access
2. PAL CTRL login
3. Palworld game access
4. Palworld administration

Do not use the sample password in production. Passwords belong only in the
server-side `.env` and Palworld configuration, never in documentation, source
control, issue reports or screenshots.
