# Linux deployment

This guide describes the recommended single-server Docker deployment. Replace
all example paths, hostnames and passwords before use.

## Requirements

- Linux VPS with at least 8 GB RAM
- Docker Engine with the Compose plugin
- Nginx
- A public UDP port for Palworld
- HTTPS certificate or a trusted private network
- Firewall and SSH access

Palworld benefits more from CPU performance and memory headroom than from a
large bandwidth allowance.

## Directory layout

The included deployment files assume:

```text
/opt/palworld/
├── .env
├── app/
├── compose.yml
├── helper.sh
└── data/
    ├── Saved/
    ├── backups/
    └── resets/
```

Create the directories:

```bash
install -d -m 750 \
  /opt/palworld/app \
  /opt/palworld/data/Saved \
  /opt/palworld/data/backups \
  /opt/palworld/data/resets
```

Copy the repository into `/opt/palworld/app`, then copy:

```bash
cp deploy/compose.yml /opt/palworld/compose.yml
cp deploy/helper.sh /opt/palworld/helper.sh
chmod 750 /opt/palworld/helper.sh
```

## Environment

Create `/opt/palworld/.env` with restrictive permissions:

```dotenv
PANEL_ADDR=0.0.0.0:8080
PANEL_PASSWORD=replace-with-a-strong-panel-password
PANEL_SECURE_COOKIES=true
PALWORLD_MOCK=false

PALWORLD_REST_URL=http://palworld:8212
PALWORLD_ADMIN_USER=admin
PALWORLD_ADMIN_PASSWORD=replace-with-the-game-admin-password
PALWORLD_RCON_ADDR=palworld:25575
PALWORLD_RCON_PASSWORD=replace-with-the-game-admin-password

PALWORLD_CONTAINER=palworld
PALWORLD_COMPOSE_DIR=/opt/palworld
PALWORLD_COMPOSE_SERVICE=palworld
PALWORLD_SAVE_DIR=/palworld-saved
PALWORLD_BACKUP_DIR=/backups
PALWORLD_SETTINGS_PATH=/palworld-config/PalWorldSettings.ini
PALWORLD_SETTINGS_STATE_DIR=/backups/settings

METRICS_INTERVAL=30s
PALWORLD_SHUTDOWN_WAIT=15
PALWORLD_SHUTDOWN_MESSAGE=Server is shutting down. See you soon.
```

```bash
chmod 600 /opt/palworld/.env
```

Never place the real environment file in Git.

## Palworld configuration

Create:

```text
/opt/palworld/data/Saved/Config/LinuxServer/PalWorldSettings.ini
```

At minimum, enable the management interfaces on the private Docker network:

```ini
RCONEnabled=True
RCONPort=25575
RESTAPIEnabled=True
RESTAPIPort=8212
bIsUseBackupSaveData=True
```

Set `AdminPassword` and `ServerPassword` to values appropriate for your group.
The REST and RCON ports must not be published publicly.

## Start

```bash
cd /opt/palworld
docker compose config -q
docker compose up -d --build
docker compose ps
```

Watch startup:

```bash
docker compose logs -f --tail 200 palworld palctrl
```

## Nginx

The Compose file binds PAL CTRL to `127.0.0.1:8080`. Use Nginx for HTTPS.

Example:

```nginx
server {
    listen 80;
    server_name panel.example.com;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl http2;
    server_name panel.example.com;

    ssl_certificate /etc/letsencrypt/live/panel.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/panel.example.com/privkey.pem;

    client_max_body_size 1m;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
        proxy_read_timeout 300s;
    }
}
```

The long proxy timeout allows settings restarts, updates and backups to finish.

If no domain is available, Nginx can serve a certificate for the IP address,
but browsers will warn unless that certificate is trusted. A private tailnet is
the preferred alternative.

## Firewall

Typical public ports:

| Port | Protocol | Purpose |
| --- | --- | --- |
| 22 | TCP | SSH, preferably restricted by source IP |
| 80 | TCP | HTTPS redirect or certificate validation |
| 443 | TCP | PAL CTRL through Nginx |
| 8211 | UDP | Palworld gameplay |

Do not expose:

- `8080/tcp` PAL CTRL origin
- `8212/tcp` Palworld REST
- `25575/tcp` RCON

## Updating PAL CTRL

```bash
cd /opt/palworld
cp -a app "app.before-update-$(date -u +%Y%m%dT%H%M%SZ)"
docker compose build palctrl
docker compose up -d --no-deps --force-recreate palctrl
docker compose ps
```

Updating only PAL CTRL does not require restarting Palworld.

## Updating Palworld

Use the panel's Update action or:

```bash
cd /opt/palworld
docker compose pull palworld
docker compose up -d palworld
```

Create a world backup before updating.

## Deployment rollback

Keep a timestamped copy of:

- `/opt/palworld/app`
- `/opt/palworld/compose.yml`
- `/opt/palworld/.env`
- `PalWorldSettings.ini`

Restore the previous app and Compose file, then rebuild only the panel:

```bash
cd /opt/palworld
docker compose build palctrl
docker compose up -d --no-deps --force-recreate palctrl
```

