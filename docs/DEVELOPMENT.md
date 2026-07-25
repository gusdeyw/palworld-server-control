# Development

## Repository layout

```text
.
├── main.go                 HTTP server, routes, auth and state aggregation
├── palworld.go             Palworld REST client and sample state
├── docker.go               Docker and native lifecycle control
├── rcon.go                 Legacy Source RCON client
├── backup.go               ZIP backup creation and listing
├── settings.go             Official settings catalog, parser and validation
├── settings_handlers.go    Apply, restart, rollback and recovery workflow
├── static/
│   ├── index.html
│   ├── app.css
│   └── app.js
├── simulator/              REST, RCON and save-data simulator
├── scripts/                Native Windows startup and memory-limit scripts
├── deploy/                 Linux Compose, Nginx and backup templates
└── docs/                   Project documentation
```

## Requirements

- Go 1.23 or newer, or Docker
- Docker for integration testing and local sample mode
- Node.js only for optional JavaScript syntax checking

There is no npm dependency tree and no frontend compilation.

## Formatting and tests

Native Go:

```bash
gofmt -w *.go
go test ./...
go vet ./...
```

Docker:

```powershell
docker run --rm -v "${PWD}:/src" -w /src golang:1.23-alpine `
  sh -c "gofmt -w *.go && go test ./... && go vet ./..."
```

JavaScript syntax:

```bash
node --check static/app.js
```

Production image:

```bash
docker build -t palctrl:local .
```

## Test coverage

The Go tests cover:

- Mandatory panel authentication
- Session enforcement
- Palworld REST aggregation and Basic Auth
- RCON packet parsing and response behavior
- ZIP backup creation and ordering
- Complete settings-catalog validity
- Preset validation
- INI parsing with nested lists and quoted commas
- Preservation of passwords and infrastructure fields
- Numeric bounds and unsafe-value rejection
- Palworld API key casing
- Baseline JSON round trips

## Local simulator

```bash
docker compose -f compose.simulator.yml up -d --build
```

The simulator provides:

- Palworld-like REST API
- RCON listener
- Sample players
- Dynamic metrics
- Sample logs
- Save directory
- Working lifecycle actions

Use it to test the interface without downloading the dedicated server.

## Sample mode

For the smallest interface preview:

```bash
docker build -t palctrl:local .
docker run --rm -p 127.0.0.1:8080:8080 \
  -e PANEL_ADDR=0.0.0.0:8080 \
  -e PANEL_PASSWORD=demo \
  -e PALWORLD_MOCK=true \
  palctrl:local
```

## Adding an official setting

1. Confirm the setting in Pocketpair's current documentation and default INI.
2. Add a complete `SettingDefinition` in `settings.go`.
3. Choose the correct group and input type.
4. Add conservative validation bounds.
5. State whether lower or higher values make the game easier.
6. Add or update a parser/validation test.
7. Verify the field on desktop and mobile.
8. Confirm infrastructure fields remain excluded.

Do not add a browser-controlled arbitrary INI key/value endpoint.

## Adding a preset

Add a `GamePreset` in `gamePresets()` using only keys from the catalog. Presets
are validated by tests.

Be careful with Pal-wide damage multipliers: they affect hostile Pals and bosses
as well as allied Pals. Boss presets therefore prefer player-specific damage
and defense controls.

## Frontend conventions

- Preserve the existing dark, compact control-room interface.
- Use native accessible form controls.
- Keep all mutations behind a confirmation step.
- Show loading, empty, error, working and disabled states.
- Avoid frontend dependencies unless they remove meaningful complexity.
- Test at 390 px and desktop widths.
- Respect `prefers-reduced-motion`.

## Windows notes

The unsigned native `palctrl.exe` may be blocked by Windows Smart App Control.
Do not disable Windows security to run it. Use the Linux executable inside the
Docker image.

The Windows Palworld server itself can be controlled by `native_agent.py`.
`Start-PalworldLimited.ps1` uses a Job Object to apply a hard memory ceiling.

## Release checklist

1. Run `gofmt`, unit tests and `go vet`.
2. Run JavaScript syntax checking.
3. Build the Docker image.
4. Test login and all views in sample mode.
5. Test desktop and mobile layouts.
6. Confirm no credentials or runtime data are in the diff.
7. Back up production app, Compose, environment and INI files.
8. Rebuild and recreate only PAL CTRL.
9. Verify HTTPS login, `/api/state` and `/api/game-settings`.
10. Confirm Palworld was not restarted by a panel-only deployment.

