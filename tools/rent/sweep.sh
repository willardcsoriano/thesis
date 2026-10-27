#!/usr/bin/env bash
# Deletes rented test servers that have outlived their run, so a box is never
# billed for time nobody used: one whose rent.sh died with the laptop asleep,
# offline or killed. It touches only servers carrying the label rent.sh puts on
# every box it creates (purpose=synapse-test), never any other server, such as
# dev.
#
#   bash tools/rent/sweep.sh            # delete labelled servers older than RENT_MAX_HOURS
#   bash tools/rent/sweep.sh --all      # delete every labelled server, whatever its age
#
# rent.sh runs it before creating a box, and install-sweeper.sh runs it every
# 30 minutes from a systemd user timer. A deletion stops the charge; a server
# that is merely stopped is still billed.
set -euo pipefail
max_hours=${RENT_MAX_HOURS:-4}
all=0
[ "${1:-}" = --all ] && all=1
label=purpose=synapse-test

servers=$(hcloud server list -l "$label" -o json) || { echo "sweep: could not list servers" >&2; exit 1; }
now=$(date +%s)
echo "$servers" | jq -r '.[] | "\(.id) \(.name) \(.created)"' | while read -r id name created; do
  age_h=$(( (now - $(date -d "$created" +%s)) / 3600 ))
  if [ $all -eq 1 ] || [ "$age_h" -ge "$max_hours" ]; then
    echo "sweep: deleting $name (id $id, $age_h h old)"
    hcloud server delete "$id" >/dev/null && echo "sweep: deleted $name"
  else
    echo "sweep: keeping $name ($age_h h old, limit $max_hours h)"
  fi
done
