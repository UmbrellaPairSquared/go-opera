#!/usr/bin/env bash -euo pipefail

declare -r DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
. "${DIR%/*}/set_globals.bash"

name="$PROJECT-net"
docker network rm "$name"
docker network create --driver=bridge --subnet="$1" "$name"
