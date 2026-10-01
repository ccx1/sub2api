#!/usr/bin/env sh
set -eu

APP_HOME=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
APP_BIN=${APP_BIN:-"$APP_HOME/sub2api"}
RUN_DIR=${RUN_DIR:-"$APP_HOME/run"}
LOG_DIR=${LOG_DIR:-/data/logs/sub2api}
PID_FILE="$RUN_DIR/sub2api.pid"
LOGGER_PID_FILE="$RUN_DIR/sub2api-logger.pid"
PIPE_FILE="$RUN_DIR/sub2api-log.pipe"
STOP_TIMEOUT=${STOP_TIMEOUT:-20}

ensure_dirs() {
  mkdir -p "$RUN_DIR" "$LOG_DIR"
}

current_log_file() {
  printf '%s/%s.log' "$LOG_DIR" "$(date '+%F')"
}

write_service_log() {
  ensure_dirs
  printf '%s [service] %s\n' "$(date '+%F %T')" "$1" >> "$(current_log_file)"
}

get_pid_or_empty() {
  file_path=$1
  if [ -f "$file_path" ]; then
    tr -d '[:space:]' < "$file_path"
  fi
}

is_running_from_file() {
  file_path=$1
  pid=$(get_pid_or_empty "$file_path")
  [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null
}

cleanup_state_files() {
  rm -f "$PID_FILE" "$LOGGER_PID_FILE"
  if [ -p "$PIPE_FILE" ]; then
    rm -f "$PIPE_FILE"
  fi
}

cleanup_stale_state() {
  if ! is_running_from_file "$PID_FILE"; then
    rm -f "$PID_FILE"
  fi

  if ! is_running_from_file "$LOGGER_PID_FILE"; then
    rm -f "$LOGGER_PID_FILE"
  fi

  if [ -p "$PIPE_FILE" ] && [ ! -f "$LOGGER_PID_FILE" ]; then
    rm -f "$PIPE_FILE"
  fi
}

start_logger() {
  rm -f "$PIPE_FILE"
  mkfifo "$PIPE_FILE"

  nohup sh -c '
    pipe_file=$1
    log_dir=$2
    while IFS= read -r line || [ -n "$line" ]; do
      log_file="$log_dir/$(date +%F).log"
      printf "%s\n" "$line" >> "$log_file"
    done < "$pipe_file"
  ' sh "$PIPE_FILE" "$LOG_DIR" >/dev/null 2>&1 &

  logger_pid=$!
  printf '%s' "$logger_pid" > "$LOGGER_PID_FILE"
}

stop_logger() {
  logger_pid=$(get_pid_or_empty "$LOGGER_PID_FILE")
  if [ -n "$logger_pid" ] && kill -0 "$logger_pid" 2>/dev/null; then
    kill "$logger_pid" 2>/dev/null || true
  fi
  rm -f "$LOGGER_PID_FILE"
  if [ -p "$PIPE_FILE" ]; then
    rm -f "$PIPE_FILE"
  fi
}

wait_for_exit() {
  pid=$1
  timeout=$2

  while [ "$timeout" -gt 0 ]; do
    if ! kill -0 "$pid" 2>/dev/null; then
      return 0
    fi
    sleep 1
    timeout=$((timeout - 1))
  done

  return 1
}

start_app() {
  ensure_dirs
  cleanup_stale_state

  if [ ! -f "$APP_BIN" ]; then
    echo "Binary not found: $APP_BIN" >&2
    exit 1
  fi

  if [ ! -x "$APP_BIN" ]; then
    echo "Binary is not executable. Run: chmod +x $APP_BIN" >&2
    exit 1
  fi

  if is_running_from_file "$PID_FILE"; then
    pid=$(get_pid_or_empty "$PID_FILE")
    echo "sub2api is already running, PID=$pid"
    exit 0
  fi

  start_logger
  nohup "$APP_BIN" > "$PIPE_FILE" 2>&1 < /dev/null &
  app_pid=$!
  printf '%s' "$app_pid" > "$PID_FILE"

  sleep 1
  if kill -0 "$app_pid" 2>/dev/null; then
    write_service_log "start pid=$app_pid app_home=$APP_HOME"
    echo "sub2api started, PID=$app_pid"
    return 0
  fi

  stop_logger
  rm -f "$PID_FILE"
  echo "sub2api failed to start, check $(current_log_file)" >&2
  exit 1
}

stop_app() {
  cleanup_stale_state
  app_pid=$(get_pid_or_empty "$PID_FILE")

  if [ -z "$app_pid" ] || ! kill -0 "$app_pid" 2>/dev/null; then
    cleanup_state_files
    echo "sub2api is not running"
    return 0
  fi

  kill "$app_pid" 2>/dev/null || true
  if ! wait_for_exit "$app_pid" "$STOP_TIMEOUT"; then
    kill -9 "$app_pid" 2>/dev/null || true
    wait_for_exit "$app_pid" 3 || true
  fi

  sleep 1
  stop_logger
  write_service_log "stop pid=$app_pid"
  rm -f "$PID_FILE"
  echo "sub2api stopped"
}

status_app() {
  cleanup_stale_state
  if is_running_from_file "$PID_FILE"; then
    pid=$(get_pid_or_empty "$PID_FILE")
    echo "sub2api is running, PID=$pid"
    exit 0
  fi

  echo "sub2api is not running"
  exit 1
}

usage() {
  cat <<'EOF'
Usage: ./scripts/sub2api-service.sh {start|stop|restart|status}

Environment variables:
  APP_BIN       Binary path, default is ../sub2api
  RUN_DIR       PID/FIFO directory, default is ../run
  LOG_DIR       Log directory, default is /data/logs/sub2api
  STOP_TIMEOUT  Graceful stop timeout in seconds, default is 20
EOF
}

command=${1:-}

case "$command" in
  start)
    start_app
    ;;
  stop)
    stop_app
    ;;
  restart)
    stop_app
    start_app
    ;;
  status)
    status_app
    ;;
  *)
    usage
    exit 1
    ;;
esac
