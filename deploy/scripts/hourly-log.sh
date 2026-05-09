#!/bin/sh
set -u

if [ "$#" -lt 2 ]; then
  echo "usage: hourly-log.sh <source> <command> [args...]" >&2
  exit 64
fi

source_name="$1"
shift

base_dir="${RAYPILOT_LOG_DIR:-/app/logs}"
fifo="${TMPDIR:-/tmp}/raypilot-log-$$.fifo"
child_pid=""

cleanup() {
  if [ -n "$child_pid" ]; then
    kill "$child_pid" 2>/dev/null || true
  fi
  rm -f "$fifo"
}

trap 'cleanup; exit 143' INT TERM
trap 'rm -f "$fifo"' EXIT

mkdir -p "$base_dir/$source_name"
rm -f "$fifo"
mkfifo "$fifo"

"$@" >"$fifo" 2>&1 &
child_pid="$!"

while IFS= read -r line; do
  date_part="$(date '+%Y-%m-%d')"
  hour_part="$(date '+%H')"
  log_dir="$base_dir/$source_name/$date_part"
  mkdir -p "$log_dir"
  printf '%s\n' "$line" | tee -a "$log_dir/$hour_part.log"
done <"$fifo"

wait "$child_pid"
status="$?"
child_pid=""
exit "$status"
