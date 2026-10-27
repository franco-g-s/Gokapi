#!/bin/bash
# Change Gokapi's maximum upload size, in GB, and restart.
#
#   ./set-max-filesize.sh 25      -> 25 GB per file
#   ./set-max-filesize.sh         -> just show the current value
#
# Also rewrites the figure shown on the public landing page so friends see the
# real limit. Gokapi enforces this both client-side (Dropzone) and server-side.
set -euo pipefail

export PATH=/opt/homebrew/bin:$PATH
# The mini runs two Docker daemons - Gokapi lives on Colima, not Docker Desktop.
export DOCKER_HOST=unix://$HOME/.colima/default/docker.sock

DIR="$HOME/apps/gokapi"
CUSTOMCSS="$DIR/custom/custom.css"
VERSIONTXT="$DIR/custom/version.txt"

current_mb=$(docker exec gokapi grep -o '"MaxFileSizeMB": [0-9]*' /app/config/config.json | grep -o '[0-9]*')
echo "current limit: ${current_mb} MB ($((current_mb / 1024)) GB)"

if [ $# -eq 0 ]; then
    free=$(docker exec gokapi df -h /app/data | tail -1 | awk '{print $4}')
    echo "free space on the data volume: ${free}"
    echo "usage: $0 <GB>"
    exit 0
fi

gb="$1"
if ! [[ "$gb" =~ ^[0-9]+$ ]] || [ "$gb" -lt 1 ]; then
    echo "error: give a whole number of GB, e.g. '$0 25'" >&2
    exit 1
fi
new_mb=$((gb * 1024))

# Warn (don't block) if the new limit exceeds what the disk can actually hold.
avail_mb=$(docker exec gokapi df -m /app/data | tail -1 | awk '{print $4}')
if [ "$new_mb" -gt "$avail_mb" ]; then
    echo "warning: ${gb} GB exceeds the ${avail_mb} MB currently free on the data volume."
    echo "         Uploads will still be refused once MIN_FREE_SPACE is reached."
fi

docker exec gokapi cp /app/config/config.json /app/config/config.json.bak
docker exec gokapi sed -i "s/\"MaxFileSizeMB\": ${current_mb}/\"MaxFileSizeMB\": ${new_mb}/" /app/config/config.json

if ! docker exec gokapi cat /app/config/config.json | python3 -c 'import sys,json; json.load(sys.stdin)' 2>/dev/null; then
    echo "error: config.json is no longer valid JSON - restoring backup" >&2
    docker exec gokapi cp /app/config/config.json.bak /app/config/config.json
    exit 1
fi

# The limit is shown under the drop box by custom.css - keep it honest, and bump
# version.txt so browsers don't serve the old file from their 2-day cache.
if [ -f "$CUSTOMCSS" ]; then
    /usr/bin/sed -i '' -E "s/(content: \")Up to [0-9]+ GB per file/\1Up to ${gb} GB per file/" "$CUSTOMCSS"
    v=$(cat "$VERSIONTXT" 2>/dev/null || echo 0)
    echo $((v + 1)) > "$VERSIONTXT"
    echo "bumped custom.css cache version to $((v + 1))"
fi

echo "restarting gokapi..."
docker compose -f "$DIR/docker-compose.yml" restart >/dev/null 2>&1
sleep 8
docker ps --filter name=gokapi --format "  {{.Names}}: {{.Status}}"
echo "new limit: ${new_mb} MB (${gb} GB)"
