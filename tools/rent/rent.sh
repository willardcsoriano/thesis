#!/usr/bin/env bash
# Rents a Hetzner server for one test job, runs it on exactly this checkout,
# brings the results back, and deletes the server, however the script ends:
# done, failed, Ctrl+C or killed. Anything it cannot delete, because the laptop
# slept or the script died, sweep.sh deletes once it is RENT_MAX_HOURS old.
#
#   bash tools/rent/rent.sh --check                       # what would be rented, at what price; creates nothing
#   bash tools/rent/rent.sh -- bash tools/layer-box/run.sh qwen2.5-coder:1.5b qwen2.5-coder:3b
#
# The command after -- runs as the box's ordinary user in ~/thesis, the copied
# checkout. Settings, from the environment:
#   RENT_TYPE       server type (default cpx52: 12 shared vCPUs, 24 GB)
#   RENT_LOCATION   location (default nbg1)
#   RENT_MAX_HOURS  the job is stopped, and the server deleted, after this (default 4)
#   RENT_FETCH      paths under the box user's home to bring back (default "layer-box")
#   RENT_SSH_KEY    Hetzner name of the key the box is created with (default synapse-rent,
#                   if Hetzner has it; else the one key there matching ~/.ssh)
#
# Results land in tools/rent/out/<UTC time>/ (git-ignored): rent.log, box.txt
# (what ran where: type, image, CPU, package and tool versions, the checkout's
# fingerprint), job.out, and the fetched paths. The procedure it automates is
# docs/testing-environment.md's.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo=$(cd "$here/../.." && pwd)
type=${RENT_TYPE:-cpx52}
location=${RENT_LOCATION:-nbg1}
max_hours=${RENT_MAX_HOURS:-4}
fetch=${RENT_FETCH:-layer-box}
label=purpose=synapse-test
firewall=verify-ssh-only

check=0
if [ "${1:-}" = --check ]; then check=1; shift; fi
[ "${1:-}" = -- ] && shift
[ $check -eq 1 ] || [ $# -gt 0 ] || { echo "usage: rent.sh --check | rent.sh -- COMMAND..." >&2; exit 2; }
job=""
[ $# -eq 0 ] || job=$(printf '%q ' "$@")
# The job is passed inside single quotes to the box's shell.
case $job in *"'"*) echo "rent: the command may not contain a single quote" >&2; exit 2;; esac

for t in hcloud jq rsync ssh ssh-keygen curl; do
  command -v "$t" >/dev/null || { echo "rent: $t is not installed" >&2; exit 1; }
done

stamp=$(date -u +%Y%m%dT%H%M%SZ)
out=$repo/tools/rent/out/$stamp
mkdir -p "$out"
exec > >(tee -a "$out/rent.log") 2>&1
say() { echo "rent: $*"; }

# --- What would be rented, and whether it can be.
price=$(hcloud server-type describe "$type" -o json | jq -r --arg l "$location" '.prices[] | select(.location == $l) | .price_hourly.net')
available=$(hcloud server-type describe "$type" -o json | jq -r --arg l "$location" '.locations[] | select(.name == $l) | .available')
cores=$(hcloud server-type describe "$type" -o json | jq -r .cores)
if [ -z "$price" ] || [ "$available" != true ]; then
  say "$type is not orderable in $location (available: ${available:-unknown})"; exit 1
fi
say "$type in $location: $cores cores, EUR $price an hour net; job limit $max_hours h"

# The key the box is created with: RENT_SSH_KEY, by its Hetzner name, when that
# key exists there (the default, synapse-rent, is a key made only for these
# boxes, without a passphrase, so a run never waits on one). Otherwise the key
# Hetzner knows that this machine holds, matched by fingerprint: exactly one,
# or stop, since guessing would create a box nobody can reach.
key=${RENT_SSH_KEY:-synapse-rent}
if ! hcloud ssh-key describe "$key" >/dev/null 2>&1; then
  local_fps=$(for f in ~/.ssh/*.pub; do ssh-keygen -l -E md5 -f "$f" 2>/dev/null | awk '{sub("MD5:", "", $2); print $2}'; done)
  key=$(hcloud ssh-key list -o json | jq -r --arg fps "$local_fps" '($fps | split("\n")) as $f | .[] | select(.fingerprint as $x | $f | index($x)) | .name')
  [ "$(printf '%s\n' "$key" | grep -c .)" -eq 1 ] || { say "need RENT_SSH_KEY, or exactly one Hetzner SSH key matching a key in ~/.ssh, found: ${key:-none}"; exit 1; }
fi
# The private key itself, passed to every ssh: a fresh box is reached by its
# bare address, which no ~/.ssh/config Host block names, so without -i ssh
# offers only the default key names and whatever the agent holds. The run of
# 2026-10-27 failed with "Permission denied (publickey)" because the agent
# was empty after a fresh login.
hetzner_fp=$(hcloud ssh-key describe "$key" -o json | jq -r .fingerprint)
key_file=""
for f in ~/.ssh/*.pub; do
  fp=$(ssh-keygen -l -E md5 -f "$f" 2>/dev/null | awk '{sub("MD5:", "", $2); print $2}')
  [ "$fp" = "$hetzner_fp" ] && [ -f "${f%.pub}" ] && key_file=${f%.pub}
done
[ -n "$key_file" ] || { say "no private key in ~/.ssh matches Hetzner key $key"; exit 1; }
# A passphrase-protected key works only through the agent, since every ssh here
# runs in batch mode. Checked before anything is rented: a box nobody can
# reach costs a billed hour and teaches nothing.
if ! ssh-keygen -y -P "" -f "$key_file" >/dev/null 2>&1; then
  key_sha=$(ssh-keygen -l -f "$key_file.pub" | awk '{print $2}')
  if ! ssh-add -l 2>/dev/null | grep -qF "$key_sha"; then
    say "$key_file has a passphrase and the ssh agent does not hold it; unlock it once, then rerun: ssh-add $key_file"
    exit 1
  fi
fi
say "ssh key: $key ($key_file)"

# Anything an earlier run left behind goes first.
RENT_MAX_HOURS=$max_hours bash "$here/sweep.sh"

if [ $check -eq 1 ]; then
  say "check only: nothing created"
  exit 0
fi

# --- The firewall accepts SSH from this machine's current address only.
public_ip() { curl -s -4 --max-time 10 https://api.ipify.org; }
admit() { # address
  printf '[{"direction":"in","protocol":"tcp","port":"22","source_ips":["%s/32"]}]' "$1" > "$out/fw.json"
  if hcloud firewall describe "$firewall" >/dev/null 2>&1; then
    hcloud firewall replace-rules "$firewall" --rules-file "$out/fw.json" >/dev/null
  else
    hcloud firewall create --name "$firewall" --rules-file "$out/fw.json" >/dev/null
  fi
  rm -f "$out/fw.json"
}
ip=$(public_ip)
[[ $ip =~ ^[0-9]{1,3}(\.[0-9]{1,3}){3}$ ]] || { say "could not read this machine's public IPv4 address"; exit 1; }
admit "$ip"
say "firewall $firewall admits this machine only"

# --- Create, and from here on always delete.
name=synapse-test-$(date -u +%Y%m%d-%H%M%S)
server_id=""
started=""
job_started=0
cleanup() {
  local code=$?
  say "stopping with status $code"
  # Whatever ended the script, the results come back before the server goes:
  # on 2026-10-26 a run ended while the laptop's network was down and the
  # server was deleted with most of an hour's results on it.
  [ "$job_started" = 0 ] || fetch_results
  if [ -n "$server_id" ]; then
    for _ in 1 2 3; do
      hcloud server delete "$server_id" >/dev/null 2>&1 && break
      sleep 5
    done
    # Deleted only when Hetzner says so: on 2026-10-27 the laptop's DNS was
    # down, both calls failed, the failed check read as "gone", and the log
    # said deleted while the server ran two more hours until sweep.sh took it.
    local gone
    gone=$(hcloud server describe "$server_id" 2>&1 >/dev/null || true)
    if hcloud server describe "$server_id" >/dev/null 2>&1; then
      say "COULD NOT DELETE $name (id $server_id): delete it in the Console or with: hcloud server delete $server_id"
    elif [[ $gone == *"not found"* ]]; then
      mins=$(( ($(date +%s) - started) / 60 ))
      say "deleted $name after $mins min; billed by the started hour, about EUR $(awk -v p="$price" -v m="$mins" 'BEGIN {printf "%.2f", p * (int(m / 60) + 1)}')"
    else
      say "COULD NOT CONFIRM $name (id $server_id) is deleted (${gone:-no answer}); sweep.sh deletes it once it is $max_hours h old, or run: hcloud server delete $server_id"
    fi
  fi
  exit "$code"
}
trap cleanup EXIT
trap 'say "interrupted (INT)"; exit 130' INT
trap 'say "terminated (TERM)"; exit 143' TERM
trap 'say "hung up (HUP)"; exit 129' HUP

server_id=$(hcloud server create --name "$name" --type "$type" --location "$location" --image debian-13 \
  --ssh-key "$key" --firewall "$firewall" --label "$label" -o json | jq -r .server.id)
started=$(date +%s)
if [ -z "$server_id" ] || [ "$server_id" = null ]; then server_id=""; say "the server was not created"; exit 1; fi
box=$(hcloud server ip "$server_id")
say "created $name (id $server_id)"

kh=$out/known_hosts
ssh_opts=(-i "$key_file" -o IdentitiesOnly=yes -o UserKnownHostsFile="$kh" -o StrictHostKeyChecking=accept-new -o ControlPath=none -o BatchMode=yes -o ConnectTimeout=10 -o ServerAliveInterval=30)
on() { local who=$1; shift; ssh -n "${ssh_opts[@]}" "$who@$box" "$@"; }
for _ in $(seq 60); do on root true 2>/dev/null && break; sleep 5; done
on root true || { say "the box never accepted SSH"; exit 1; }

# --- This checkout, exactly, then the documented set-up.
BOX_SSH="ssh ${ssh_opts[*]}" bash "$repo/tools/break-box/push.sh" "$box" >/dev/null
scp -q "${ssh_opts[@]}" "$repo/tools/verify-box/setup.sh" "root@$box:"
say "setting up (setup.sh --copied)"
on root 'bash setup.sh --copied' > "$out/setup.log" 2>&1 || { say "set-up failed; see setup.log"; exit 1; }
grep -q "sandbox ok" "$out/setup.log" || { say "the sandbox check did not pass; see setup.log"; exit 1; }

fingerprint() { (git diff HEAD --binary; git ls-files -o --exclude-standard -z | xargs -0r sha256sum) | sha256sum | cut -c1-12; }
here_fp="$(git -C "$repo" rev-parse HEAD)+$(cd "$repo" && fingerprint)"
box_fp=$(on verify "cd thesis && echo \$(git rev-parse HEAD)+\$( (git diff HEAD --binary; git ls-files -o --exclude-standard -z | xargs -0r sha256sum) | sha256sum | cut -c1-12)")
[ "$here_fp" = "$box_fp" ] || { say "the box's tree ($box_fp) is not this checkout ($here_fp)"; exit 1; }

# What ran where, for anyone reproducing the result.
# shellcheck disable=SC2016 # expanded on the box, not here
{
  echo "checkout $here_fp"
  echo "server $type in $location, image $(hcloud server describe "$server_id" -o json | jq -r '.image.name + " (id " + (.image.id | tostring) + ")"')"
  on verify 'echo "cpu $(lscpu | sed -n "s/^Model name: *//p") x $(nproc)"; awk "/^MemTotal/ {printf \"memory %d MB\n\", \$2/1024}" /proc/meminfo; echo "kernel $(uname -r)"; . /etc/os-release; echo "os $PRETTY_NAME"; echo "go $(~/.local/go/bin/go version | cut -d" " -f3)"; echo "ollama pin $(cat thesis/distro/ollama-pin | cut -d" " -f1)"; dpkg-query -W -f "pkg \${Package} \${Version}\n" bubblewrap bash coreutils passt git 2>/dev/null'
} > "$out/box.txt"
cat "$out/box.txt"

# The results so far, copied over what an earlier copy brought. Bounded, so a
# dead connection cannot hold up the delete.
fetch_results() {
  timeout 300 rsync -a -e "ssh ${ssh_opts[*]}" "verify@$box:job.out" "$out/" 2>/dev/null || say "job.out not copied"
  for p in $fetch; do
    timeout 300 rsync -a --exclude synapse.test -e "ssh ${ssh_opts[*]}" "verify@$box:$p" "$out/" 2>/dev/null || say "$p not copied"
  done
}

# --- The job, detached on the box so a dropped connection does not stop it,
# and bounded by RENT_MAX_HOURS. While it runs, the results so far are copied
# back every 5 minutes, and if this machine's address changes (a network that
# dropped and came back), the firewall is pointed at the new one.
say "running: $job"
# Only setsid goes to the background: in "cd thesis && setsid … &" the & takes
# the whole list, and the subshell running it kept the SSH session open until
# the job ended, so this call waited out the job (2026-10-26). A network drop
# then ended it, set -e ended the script, and the server was deleted.
on verify "cd thesis && { setsid nohup bash -c 'timeout ${max_hours}h $job > ~/job.out 2>&1; echo \$? > ~/job.exit' > /dev/null 2>&1 < /dev/null & }"
job_started=1
deadline=$(( started + max_hours * 3600 ))
polls=0
failures=0
while :; do
  sleep 30
  polls=$((polls + 1))
  if code=$(on verify 'cat job.exit 2>/dev/null; true'); then
    failures=0
  else
    code=""
    failures=$((failures + 1))
    say "the box did not answer ($failures in a row); the job runs on regardless"
    now_ip=$(public_ip || true)
    if [[ $now_ip =~ ^[0-9]{1,3}(\.[0-9]{1,3}){3}$ ]] && [ "$now_ip" != "$ip" ]; then
      admit "$now_ip" && ip=$now_ip && say "this machine's address changed; the firewall admits the new one"
    fi
  fi
  [ -n "$code" ] && break
  [ "$(date +%s)" -lt "$deadline" ] || { say "the job passed $max_hours h"; break; }
  [ $((polls % 10)) -ne 0 ] || [ "$failures" -ne 0 ] || fetch_results
done
say "job exit status: ${code:-none}"

# --- Results back, then the trap deletes the server (and fetches once more).
fetch_results
job_started=0
say "results in $out"
[ "${code:-1}" = 0 ]
