#!/bin/sh
set -eu

mkdir -p /music-beets/inbox /music-beets/library

cat >/config/sldl.conf <<EOF
username = ${SLSK_USERNAME}
password = ${SLSK_PASSWORD}
pref-format = flac,mp3
path = /music-beets/inbox
fast-search = false
connect-timeout = 60000
searches-per-time = 12
searches-renew-time = 300
EOF

mkdir -p /config/processed /data/queue/weekly-queue /data/queue/playlist-processed
touch /config/index.sldl

normalize_list_file() {
  input_file="$1"
  output_file="$2"
  : >"$output_file"
  while IFS= read -r line || [ -n "$line" ]; do
    trimmed="$(printf '%s' "$line" | sed 's/^[[:space:]]*//; s/[[:space:]]*$//')"
    [ -z "$trimmed" ] && continue
    case "$trimmed" in
      \#*|\"*|a:\"*)
        printf '%s\n' "$trimmed" >>"$output_file"
        ;;
      *)
        escaped="$(printf '%s' "$trimmed" | sed 's/"/\\"/g')"
        printf '"%s"\n' "$escaped" >>"$output_file"
        ;;
    esac
  done <"$input_file"
}

requested_track_title() {
  queue_file="$1"
  line="$(awk 'NF { print; exit }' "$queue_file" 2>/dev/null || true)"
  line="$(printf '%s' "$line" | sed 's/^"//; s/"$//; s/^[[:space:]]*//; s/[[:space:]]*$//')"
  case "$line" in
    *" - "*)
      printf '%s' "${line#* - }"
      ;;
    *)
      printf '%s' "$line"
      ;;
  esac
}

escape_find_pattern() {
  printf '%s' "$1" | sed 's/[][?*]/\\&/g; s#/# #g'
}

matching_track_exists() {
  search_root="$1"
  queue_file="$2"
  [ -d "$search_root" ] || return 1

  title="$(requested_track_title "$queue_file")"
  [ -n "$title" ] || return 1

  pattern="$(escape_find_pattern "$title")"
  find "$search_root" -type f ! -name '*.incomplete' -iname "*${pattern}*" -print -quit 2>/dev/null | grep -q .
}

while true; do
  find /data/queue/playlist-processed -type f \( -name '*.done' -o -name '*.failed' \) -mtime +30 -delete || true
  for f in /data/queue/weekly-queue/*.txt; do
    [ -f "$f" ] || continue
    base="$(basename "$f")"
    done_stamp="/data/queue/playlist-processed/${base}.done"
    failed_stamp="/data/queue/playlist-processed/${base}.failed"
    [ -f "$done_stamp" ] && continue
    if [ -f "$failed_stamp" ]; then
      failed_age_min="$(find "$failed_stamp" -mmin +30 -print | wc -l)"
      [ "$failed_age_min" -eq 0 ] && continue
      rm -f "$failed_stamp"
    fi
    normalized="/tmp/${base}.normalized"
    run_log="/tmp/${base}.sldl.log"
    normalize_list_file "$f" "$normalized"
    if sldl --config /config/sldl.conf --input-type list "$normalized" --index-path /config/index.sldl -p /music-beets/inbox >"$run_log" 2>&1; then
      sldl_status=0
    else
      sldl_status=$?
    fi
    cat "$run_log"

    if matching_track_exists "/music-beets/inbox/${base}" "$f" || matching_track_exists "/music-beets/library" "$f"; then
      touch "$done_stamp"
      rm -f "$failed_stamp"
    elif grep -qi "not found" "$run_log" || [ "$sldl_status" -ne 0 ]; then
      touch "$failed_stamp"
      sleep 90
    else
      echo "No usable matching track found for ${base}; leaving queue item active for retry." >&2
      sleep 90
    fi
    rm -f "$normalized" "$run_log"
  done
  sleep 60
done
