#!/bin/sh
set -eu

mkdir -p /music-beets/inbox /music-beets/library /config-data
status_dir="/config-data/status"
mkdir -p "$status_dir"

while true; do
  count="$(find /music-beets/inbox -type f | wc -l | tr -d ' ')"
  if [ "$count" -gt 0 ]; then
    run_log="${status_dir}/importer-last.log"
    now="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    if beet -c /config/config.yaml import /music-beets/inbox >"$run_log" 2>&1; then
      printf '%s success inbox_files=%s\n' "$now" "$count" >"${status_dir}/importer-last-status.txt"
      rm -f "${status_dir}/importer-last-error.log"
      cat "$run_log"
    else
      printf '%s failure inbox_files=%s\n' "$now" "$count" >"${status_dir}/importer-last-status.txt"
      cp "$run_log" "${status_dir}/importer-last-error.log"
      cat "$run_log" >&2
    fi
  fi
  sleep 120
done
