#!/usr/bin/env sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PROJECT_DIR=${PROJECT_DIR:-$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)}
OUTPUT_ROOT=${OUTPUT_ROOT:-"$PROJECT_DIR/output"}
PACKAGE_NAME=${PACKAGE_NAME:-sub2api-linux-bin}
OUTPUT_DIR="$OUTPUT_ROOT/$PACKAGE_NAME"
ARCHIVE_PATH="$OUTPUT_ROOT/$PACKAGE_NAME.tar.gz"

mkdir -p "$OUTPUT_ROOT"
rm -rf "$OUTPUT_DIR"
mkdir -p "$OUTPUT_DIR"

cd "$PROJECT_DIR"

echo "[step] Build backend binary"
go build -tags embed -o sub2api ./cmd/server

echo "[step] Prepare runtime package"
cp ./sub2api "$OUTPUT_DIR/sub2api"
cp ./config.example.yaml "$OUTPUT_DIR/config.example.yaml"
mkdir -p "$OUTPUT_DIR/scripts"
cp "$SCRIPT_DIR/sub2api-service.sh" "$OUTPUT_DIR/scripts/sub2api-service.sh"
cp "$SCRIPT_DIR/cleanup-sub2api-logs.sh" "$OUTPUT_DIR/scripts/cleanup-sub2api-logs.sh"
chmod +x "$OUTPUT_DIR/sub2api" "$OUTPUT_DIR/scripts/sub2api-service.sh" "$OUTPUT_DIR/scripts/cleanup-sub2api-logs.sh"

cat > "$OUTPUT_DIR/BUILD_INFO.txt" <<'EOF'
binary_build_command=go build -tags embed -o sub2api ./cmd/server
service_start=./scripts/sub2api-service.sh start
log_cleanup=./scripts/cleanup-sub2api-logs.sh
EOF

if [ -f "$ARCHIVE_PATH" ]; then
  rm -f "$ARCHIVE_PATH"
fi

echo "[step] Create tar.gz archive"
tar czf "$ARCHIVE_PATH" -C "$OUTPUT_ROOT" "$PACKAGE_NAME"

echo "Runtime directory: $OUTPUT_DIR"
echo "Archive:           $ARCHIVE_PATH"
