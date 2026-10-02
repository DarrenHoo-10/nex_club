#!/bin/sh
set -eu
# Run from the directory containing compose.yaml and secrets/.
umask 077
mkdir -p backups
stamp=$(date -u +%Y%m%dT%H%M%SZ)
backup_path="backups/nex-club-${stamp}.dump"
docker compose exec -T postgres pg_dump -U nex_owner -d nex_club -Fc > "${backup_path}.partial"
mv "${backup_path}.partial" "$backup_path"
printf '%s\n' "$backup_path"
