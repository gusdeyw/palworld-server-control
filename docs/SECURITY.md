# Security

PAL CTRL can start and stop a game server, read save data, write configuration
and access the Docker socket. Treat it as an administrative application.

## Threat model

The expected deployment is:

- One server
- A small trusted friend group
- One shared panel password
- No public registration
- No untrusted plugins
- No arbitrary shell access

The application is not designed for anonymous users, public hosting customers,
multiple administrators with separate roles or hostile multi-tenancy.

## Network exposure

Publicly expose only:

- HTTPS through Nginx
- Palworld's UDP game port
- SSH when required, preferably restricted by source IP or VPN

Keep these private:

- PAL CTRL origin port `8080/tcp`
- Palworld REST `8212/tcp`
- RCON `25575/tcp`
- Native Windows bridge `8213/tcp`
- Docker daemon

The included Compose deployment binds PAL CTRL to localhost and places REST and
RCON on a private Docker network.

## Authentication

PAL CTRL requires `PANEL_PASSWORD` at startup. A successful login sets:

- HttpOnly cookie
- SameSite Strict
- Secure flag when `PANEL_SECURE_COOKIES=true`
- Thirty-day maximum age

The session secret is random and held in memory. Restarting PAL CTRL invalidates
all active sessions.

Mutation requests also require `X-Pal-Control: 1`. This is defense in depth and
not a replacement for authentication.

## Password recommendations

Use separate, strong values for:

- VPS root or administrative SSH account
- Panel login
- Game join password
- Palworld admin and REST password

Do not:

- Commit passwords to Git
- Put credentials in Markdown examples
- Paste `.env` into public issues
- Expose secrets in screenshots
- Reuse the simulator password in production

If a credential is posted in chat, issue trackers or shell history, rotate it.

## Docker socket

The Docker socket grants host-equivalent control. Risks are reduced by:

- Keeping PAL CTRL private
- Requiring authentication
- Omitting arbitrary command endpoints
- Using fixed Docker operations
- Mounting save data read-only
- Mounting only the Palworld configuration directory read/write

For stronger isolation, replace direct socket access with a narrowly scoped host
agent similar to the Windows bridge.

## Filesystem safety

The settings endpoint accepts only keys present in the official catalog.
Submitted values are type checked, bounded and encoded by the server.

The INI update process:

- Preserves unknown fields
- Preserves passwords and network fields
- Handles nested lists and quoted commas
- Writes through a temporary file
- Renames the completed file atomically
- Snapshots the previous configuration

The web application has no arbitrary path parameter, upload endpoint or file
browser.

## HTTP protections

Responses set:

- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY`
- `Referrer-Policy: no-referrer`
- Restrictive `Permissions-Policy`
- Same-origin Content Security Policy

Nginx should terminate TLS and may add HSTS after HTTPS has been verified.

## Firewall and intrusion controls

Recommended host controls:

- Default-deny firewall
- SSH keys instead of password authentication
- Root login disabled after an administrative user is established
- Fail2ban or provider equivalent
- Security updates
- Restricted source IPs or a private tailnet

## Backups

Backups can contain player identifiers, world data and configuration. Protect
them with the same care as the live server:

- Restrictive filesystem permissions
- No public web directory
- Encrypted off-server copies when possible
- Retention and deletion policy
- Restore testing

## RCON

Pocketpair marks RCON as deprecated. It remains only for the legacy free-form
console. Do not expose the RCON port. Prefer official REST actions and remove
the console when RCON support disappears.

## Reporting a vulnerability

Do not include live credentials, save archives or personally identifying player
data in an issue. Provide a minimal reproduction with sample values.

