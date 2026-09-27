#!/bin/bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
TEST_ROOT=$(mktemp -d)
trap 'rm -rf -- "$TEST_ROOT"' EXIT
export INSTALLER="$ROOT_DIR/deploy/install.sh"

fail() { echo "FAIL: $*" >&2; exit 1; }
write_binary() {
    printf '%s\n' '#!/bin/bash' "echo 'Sub2API $2'" > "$1"
    chmod +x "$1"
}

prepare_case() {
    export CASE_DIR="$TEST_ROOT/$1"
    export TEST_VERSION="${2:-v0.2.8.24}"
    export OLD_VERSION="${3:-v0.2.8.23}"
    export TEST_MODE=success ACTION=upgrade TEST_OS=Linux TEST_ARCH=x86_64
    mkdir -p "$CASE_DIR/install/data" "$CASE_DIR/payload/deploy"
    write_binary "$CASE_DIR/install/sub2api" "$OLD_VERSION"
    write_binary "$CASE_DIR/payload/sub2api" "$TEST_VERSION"
    printf 'runtime-config\n' > "$CASE_DIR/install/config.yaml"
    printf 'installed-marker\n' > "$CASE_DIR/install/.installed"
    printf 'runtime-data\n' > "$CASE_DIR/install/data/keep"
    printf 'do-not-install\n' > "$CASE_DIR/payload/deploy/config.yaml"
    printf 'active\n' > "$CASE_DIR/service-state"
    : > "$CASE_DIR/calls"
    make_archive
}

make_archive() {
    local archive="sub2api_${TEST_VERSION#v}_linux_amd64.tar.gz"
    tar -czf "$CASE_DIR/$archive" -C "$CASE_DIR/payload" sub2api deploy
    (cd "$CASE_DIR" && sha256sum "$archive") > "$CASE_DIR/checksums.txt"
}

cat > "$TEST_ROOT/runner.sh" <<'RUNNER'
#!/bin/bash
set -euo pipefail
source <(head -n -1 "$INSTALLER")
INSTALL_DIR="$CASE_DIR/install"
LANG_CHOICE=en
OS=linux ARCH=amd64

curl() {
    local arg url= output= http=false
    while [ "$#" -gt 0 ]; do
        arg=$1; shift
        case "$arg" in
            -o) output=$1; shift ;;
            -w) http=true; shift ;;
            https://*) url=$arg ;;
        esac
    done
    printf 'curl %s\n' "$url" >> "$CASE_DIR/calls"
    [[ "$url" == https://api.github.com/repos/ccx1/sub2api/* ||
       "$url" == https://github.com/ccx1/sub2api/releases/download/* ]] || return 91
    case "$url" in
        */releases/latest) printf '{"tag_name":"%s"}\n' "$TEST_VERSION" ;;
        */releases/tags/*) [ "$http" = true ]; printf 200 ;;
        */checksums.txt)
            [ "$TEST_MODE" != missing-checksum ] || return 22
            cp "$CASE_DIR/checksums.txt" "$output" ;;
        *.tar.gz)
            [ "$TEST_MODE" != missing-archive ] || return 22
            cp "$CASE_DIR/${url##*/}" "$output" ;;
        *) return 92 ;;
    esac
}

systemctl() {
    printf 'systemctl %s\n' "$*" >> "$CASE_DIR/calls"
    case "$1" in
        is-active) [ "$(cat "$CASE_DIR/service-state")" = active ] ;;
        stop) printf 'inactive\n' > "$CASE_DIR/service-state" ;;
        start)
            if [ "$TEST_MODE" = start-failure ] && [ "$(get_current_version)" = "$TEST_VERSION" ]; then
                return 1
            fi
            printf 'active\n' > "$CASE_DIR/service-state" ;;
        *) return 93 ;;
    esac
}
chown() { :; }
uname() { case "$1" in -s) echo "$TEST_OS" ;; -m) echo "$TEST_ARCH" ;; esac; }
select_language() { :; }
check_root() { :; }
configure_server() { echo unexpected-runtime-reconfiguration >&2; exit 94; }

case "$ACTION" in
    upgrade) upgrade ;;
    version) install_version "$TEST_VERSION" ;;
    current) get_current_version ;;
    platform) detect_platform ;;
    fresh) LATEST_VERSION=$TEST_VERSION; download_and_extract ;;
    install) main install ;;
    default) main ;;
    rollback) main rollback "$TEST_VERSION" ;;
esac
RUNNER

run_ok() {
    bash "$TEST_ROOT/runner.sh" > "$CASE_DIR/output" 2>&1 || { cat "$CASE_DIR/output"; fail "$1"; }
    echo "PASS: $1"
}
run_failure() {
    if bash "$TEST_ROOT/runner.sh" > "$CASE_DIR/output" 2>&1; then fail "$1 accepted"; fi
    echo "PASS: $1 rejected"
}
assert_preserved() {
    [ "$(cat "$CASE_DIR/install/config.yaml")" = runtime-config ] || fail config
    [ "$(cat "$CASE_DIR/install/.installed")" = installed-marker ] || fail installed
    [ "$(cat "$CASE_DIR/install/data/keep")" = runtime-data ] || fail data
}
assert_running_old() {
    [ "$("$CASE_DIR/install/sub2api" --version)" = "Sub2API $OLD_VERSION" ] || fail old-binary
    [ "$(cat "$CASE_DIR/service-state")" = active ] || fail old-service
    assert_preserved
}
assert_before_stop_failure() {
    run_failure "$1"
    ! grep -q 'systemctl stop' "$CASE_DIR/calls" || fail premature-stop
    assert_running_old
}

prepare_case current-four
ACTION=current
run_ok 'four-segment current version'
[ "$(cat "$CASE_DIR/output")" = v0.2.8.23 ] || fail fourth-segment
prepare_case current-three v0.2.9 v0.2.8
ACTION=current
run_ok 'legacy three-segment current version'
[ "$(cat "$CASE_DIR/output")" = v0.2.8 ] || fail legacy-version

prepare_case upgrade
run_ok 'latest release upgrade'
[ "$("$CASE_DIR/install/sub2api" --version)" = 'Sub2API v0.2.8.24' ] || fail upgraded-version
[ "$("$CASE_DIR/install/sub2api.backup" --version)" = 'Sub2API v0.2.8.23' ] || fail backup
checksum_line=$(grep -n '/checksums.txt' "$CASE_DIR/calls" | cut -d: -f1)
stop_line=$(grep -n 'systemctl stop' "$CASE_DIR/calls" | cut -d: -f1)
[ "$checksum_line" -lt "$stop_line" ] || fail download-before-stop
assert_preserved

prepare_case legacy-upgrade v0.2.9 v0.2.8
run_ok 'legacy three-segment upgrade'
prepare_case rollback v0.2.8.22 v0.2.8.23
ACTION=rollback
run_ok 'specified rollback from own repository'
[ "$("$CASE_DIR/install/sub2api" --version)" = 'Sub2API v0.2.8.22' ] || fail rollback-version
assert_preserved
prepare_case same-version v0.2.8.23
ACTION=version
run_ok 'same version makes no changes'
! grep -q 'systemctl|/releases/download/' "$CASE_DIR/calls" || fail same-version-mutated

for mode in missing-archive missing-checksum; do
    prepare_case "$mode"
    TEST_MODE=$mode
    assert_before_stop_failure "$mode"
done
prepare_case bad-checksum
printf '%064d  sub2api_0.2.8.24_linux_amd64.tar.gz\n' 0 > "$CASE_DIR/checksums.txt"
assert_before_stop_failure 'wrong checksum'
prepare_case wrong-asset-checksum
sed 's/.tar.gz/.tar.gz.extra/' "$CASE_DIR/checksums.txt" > "$CASE_DIR/checksums-wrong.txt"
cp "$CASE_DIR/checksums-wrong.txt" "$CASE_DIR/checksums.txt"
assert_before_stop_failure 'checksum filename must match exactly'
prepare_case duplicate-checksum
cat "$CASE_DIR/checksums.txt" "$CASE_DIR/checksums.txt" > "$CASE_DIR/duplicates.txt"
cp "$CASE_DIR/duplicates.txt" "$CASE_DIR/checksums.txt"
assert_before_stop_failure 'ambiguous checksum entries'
prepare_case missing-binary
tar -czf "$CASE_DIR/sub2api_0.2.8.24_linux_amd64.tar.gz" -C "$CASE_DIR/payload" deploy
(cd "$CASE_DIR" && sha256sum sub2api_0.2.8.24_linux_amd64.tar.gz) > "$CASE_DIR/checksums.txt"
assert_before_stop_failure 'missing binary in archive'
prepare_case wrong-binary-version
write_binary "$CASE_DIR/payload/sub2api" v0.2.8.2
make_archive
assert_before_stop_failure 'binary and release version mismatch'
prepare_case start-failure
TEST_MODE=start-failure
run_failure 'new service startup failure'
assert_running_old
grep -q 'restoring previous binary' "$CASE_DIR/output" || fail missing-restore

for action in install default; do
    prepare_case "existing-$action"
    ACTION=$action
    run_ok "$action preserves existing service configuration"
    assert_preserved
done
prepare_case fresh
ACTION=fresh
rm -f -- "$CASE_DIR/install/sub2api"
run_ok 'fresh verified binary installation'
assert_preserved

for platform in linux-amd64 linux-arm64 darwin-amd64; do
    prepare_case "platform-$platform"
    ACTION=platform
    case "$platform" in
        linux-amd64) run_ok "$platform supported" ;;
        linux-arm64) TEST_ARCH=aarch64; run_failure "$platform" ;;
        darwin-amd64) TEST_OS=Darwin; run_failure "$platform" ;;
    esac
done
echo 'installer release checks passed'
