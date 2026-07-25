#!/bin/sh
set -eu

cd /opt/palworld

docker exec palctrl sh -c \
  'auth="$(printf "%s:%s" "$PALWORLD_ADMIN_USER" "$PALWORLD_ADMIN_PASSWORD" | base64 | tr -d "\n")"; wget -qO- --header="Authorization: Basic $auth" --post-data="" http://palworld:8212/v1/api/save >/dev/null' \
  || true
sleep 5

stamp="$(date -u +%Y%m%d-%H%M%S)"
archive="/opt/palworld/data/backups/scheduled-${stamp}.tar.gz"
tar -czf "$archive" -C /opt/palworld/data/Saved .

find /opt/palworld/data/backups \
  -type f \
  -name 'scheduled-*.tar.gz' \
  -mtime +7 \
  -delete

printf 'Created %s\n' "$archive"
