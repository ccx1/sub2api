#!/usr/bin/env sh
set -eu

LOG_DIR=${LOG_DIR:-/data/logs/sub2api}
RETENTION_DAYS=${1:-7}

case "$RETENTION_DAYS" in
  ''|*[!0-9]*)
    echo "Retention days must be a non-negative integer: $RETENTION_DAYS" >&2
    exit 1
    ;;
esac

if [ "$RETENTION_DAYS" -le 0 ]; then
  echo "Retention days must be greater than 0" >&2
  exit 1
fi

if [ ! -d "$LOG_DIR" ]; then
  echo "Log directory does not exist, skip cleanup: $LOG_DIR"
  exit 0
fi

# Keep the most recent N days including today, and delete older yyyy-MM-dd.log files.
cutoff_date=$(date -d "$((RETENTION_DAYS - 1)) days ago" '+%F')

find "$LOG_DIR" -maxdepth 1 -type f -name '*.log' | while IFS= read -r log_file; do
  log_name=$(basename "$log_file" .log)
  case "$log_name" in
    ????-??-??)
      if [ "$log_name" \< "$cutoff_date" ]; then
        echo "Delete old log: $log_file"
        rm -f "$log_file"
      fi
      ;;
  esac
done
