#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
SSHGO="$PROJECT_DIR/sshgo"

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

PASS=0
FAIL=0
TESTS=()

# Test helpers
pass() { ((PASS++)) || true; echo -e "  ${GREEN}✓ PASS${NC}: $1"; TESTS+=("PASS: $1"); }
fail() { ((FAIL++)) || true; echo -e "  ${RED}✗ FAIL${NC}: $1"; TESTS+=("FAIL: $1"); }
assert_eq() {
    local desc="$1" expected="$2" actual="$3"
    if [[ "$expected" == "$actual" ]]; then
        pass "$desc"
    else
        fail "$desc (expected: '$expected', got: '$actual')"
    fi
}
assert_contains() {
    local desc="$1" expected="$2" actual="$3"
    if echo "$actual" | grep -qF "$expected"; then
        pass "$desc"
    else
        fail "$desc (expected to contain: '$expected', got: '$actual')"
    fi
}
assert_not_contains() {
    local desc="$1" not_expected="$2" actual="$3"
    if ! echo "$actual" | grep -qF "$not_expected"; then
        pass "$desc"
    else
        fail "$desc (expected NOT to contain: '$not_expected')"
    fi
}
# Extra safety: disable exit on error during tests so we capture failures
disable_strict() { set +e; }
enable_strict() { set -e; }

echo -e "${CYAN}========================================${NC}"
echo -e "${CYAN}  sshgo E2E Test Suite${NC}"
echo -e "${CYAN}========================================${NC}"
echo ""

# ============================================================================
# PHASE 0: Update Subcommand Tests
# ============================================================================
echo -e "${YELLOW}[Phase 0] Update subcommand tests${NC}"
echo ""

# Ensure the sshgo binary is rebuilt with the update subcommand
if command -v go &>/dev/null; then
    echo "  Rebuilding sshgo with update subcommand..."
    (cd "$PROJECT_DIR" && GOPROXY=off GONOSUMCHECK='*' GONOSUMDB='*' go build -ldflags="-s -w" -o "$SSHGO" . 2>/dev/null) && \
        echo -e "  ${GREEN}✓ sshgo rebuilt${NC}" || \
        echo -e "  ${YELLOW}⚠ Using existing sshgo binary${NC}"
fi

if ! "$SSHGO" update --help 2>&1 | grep -q "Usage: sshgo update"; then
    echo -e "  ${RED}✗ sshgo binary does not support 'update' subcommand. Rebuild required.${NC}"
    echo "  Run: cd $PROJECT_DIR && go build -o $SSHGO ."
    exit 1
fi

# Save a copy of the original binary for tests that should not replace it
ORIG_BIN="$(mktemp -t sshgo-orig-XXXXX)"
cp "$SSHGO" "$ORIG_BIN"
echo ""

# ------------------------------------------------------------------
# 0a: Update --help
# ------------------------------------------------------------------
echo "  --- Update Help ---"

OUTPUT=$("$ORIG_BIN" update --help 2>&1 || true)
assert_contains "update --help: shows usage" "Usage: sshgo update" "$OUTPUT"
assert_contains "update --help: mentions env var" "SSHGO_UPDATE_SERVER" "$OUTPUT"

OUTPUT=$("$ORIG_BIN" update -h 2>&1 || true)
assert_contains "update -h: shows usage" "Usage: sshgo update" "$OUTPUT"

# ------------------------------------------------------------------
# 0b: Mock update server setup
# ------------------------------------------------------------------
echo "  --- Mock Server Setup ---"

# Create a mock "binary" that the server will serve
MOCK_BIN="$(mktemp -t sshgo-mock-bin-XXXXX)"
cat > "$MOCK_BIN" <<'MOCKEOF'
#!/bin/sh
echo "mock sshgo v0.0.1"
MOCKEOF
chmod +x "$MOCK_BIN"

# Determine OS/arch for artifact name
UNAME_S=$(uname -s | tr '[:upper:]' '[:lower:]')
UNAME_M=$(uname -m)
case "$UNAME_M" in
    x86_64|amd64) MOCK_ARCH="amd64" ;;
    aarch64|arm64) MOCK_ARCH="arm64" ;;
    *) MOCK_ARCH="$UNAME_M" ;;
esac
case "$UNAME_S" in
    linux) MOCK_OS="linux" ;;
    darwin) MOCK_OS="darwin" ;;
    *) MOCK_OS="$UNAME_S" ;;
esac
ARTIFACT_NAME="sshgo-${MOCK_OS}-${MOCK_ARCH}"

# Python mock HTTP server script
MOCK_SERVER_SCRIPT="$(mktemp -t sshgo-mock-server-XXXXX)"
cat > "$MOCK_SERVER_SCRIPT" <<'PYEOF'
import sys
from http.server import HTTPServer, BaseHTTPRequestHandler

PORT = int(sys.argv[1])
BIN_PATH = sys.argv[2]
ARTIFACT = sys.argv[3]

class MockHandler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == '/releases/latest':
            self.send_response(302)
            self.send_header('Location', '/releases/tag/v0.0.1')
            self.end_headers()
        elif ARTIFACT in self.path and '/releases/download/' in self.path:
            self.send_response(200)
            self.send_header('Content-Type', 'application/octet-stream')
            self.end_headers()
            with open(BIN_PATH, 'rb') as f:
                self.wfile.write(f.read())
        else:
            self.send_response(404)
            self.end_headers()
            self.wfile.write(b'not found')
    def log_message(self, format, *args):
        pass

HTTPServer(('127.0.0.1', PORT), MockHandler).serve_forever()
PYEOF

# Find a free port
for try_port in 18923 18924 18925 18926 18927 18928 18929 18930; do
    if ! lsof -i :$try_port >/dev/null 2>&1; then
        MOCK_PORT=$try_port
        break
    fi
done
MOCK_PORT="${MOCK_PORT:-18931}"

echo "  Starting mock update server on port $MOCK_PORT..."
python3 "$MOCK_SERVER_SCRIPT" "$MOCK_PORT" "$MOCK_BIN" "$ARTIFACT_NAME" &
MOCK_SERVER_PID=$!
sleep 1

if ! kill -0 "$MOCK_SERVER_PID" 2>/dev/null; then
    echo -e "  ${RED}✗ FAIL${NC}: mock server failed to start"
    exit 1
fi
echo -e "  ${GREEN}✓ Mock server running (PID $MOCK_SERVER_PID)${NC}"

MOCK_URL="http://127.0.0.1:${MOCK_PORT}"

disable_strict

# ------------------------------------------------------------------
# 0c: Dev build update via copy (so $SSHGO stays intact)
# ------------------------------------------------------------------
echo "  --- Dev Build Update ---"

DEV_BIN="$(mktemp -t sshgo-dev-XXXXX)"
cp "$ORIG_BIN" "$DEV_BIN"

OUTPUT=$(SSHGO_UPDATE_SERVER="$MOCK_URL" "$DEV_BIN" update 2>&1 || true)
assert_contains "update (dev): detects dev build" "Development build detected" "$OUTPUT"
assert_contains "update (dev): downloads latest" "v0.0.1" "$OUTPUT"
assert_not_contains "update (dev): no error" "Failed to check for updates" "$OUTPUT"
assert_not_contains "update (dev): no error" "Download failed" "$OUTPUT"

# Verify the binary was actually replaced with mock content
DEV_CONTENT=$("$DEV_BIN" 2>&1 || true)
assert_contains "update (dev): binary was replaced" "mock sshgo v0.0.1" "$DEV_CONTENT"

# ------------------------------------------------------------------
# 0d: Binary with older version (v0.0.0) -> updates to v0.0.1
# ------------------------------------------------------------------
echo "  --- Outdated Binary Update ---"

OLD_BIN="$(mktemp -t sshgo-old-XXXXX)"
if command -v go &>/dev/null; then
    echo "  Building test binary with version v0.0.0..."
    (cd "$PROJECT_DIR" && GOPROXY=off GONOSUMCHECK='*' GONOSUMDB='*' go build -ldflags="-X main.version=v0.0.0" -o "$OLD_BIN" . 2>/dev/null) && \
        echo -e "  ${GREEN}✓ Built test binary v0.0.0${NC}" || \
        echo -e "  ${YELLOW}⚠ go build failed, skipping${NC}"
    
    if [ -x "$OLD_BIN" ] && "$OLD_BIN" update --help 2>/dev/null | grep -q "sshgo update"; then
        OUTPUT=$(SSHGO_UPDATE_SERVER="$MOCK_URL" "$OLD_BIN" update 2>&1 || true)
        assert_contains "update (old): sees newer version" "Updating from v0.0.0 to v0.0.1" "$OUTPUT"
        assert_contains "update (old): success message" "Successfully updated to v0.0.1" "$OUTPUT"
        
        # Verify binary was replaced
        UPDATED_CONTENT=$("$OLD_BIN" 2>&1 || true)
        assert_contains "update (old): replaced binary works" "mock sshgo v0.0.1" "$UPDATED_CONTENT"
    fi
fi

# ------------------------------------------------------------------
# 0e: Binary with same version as latest -> already up to date
# ------------------------------------------------------------------
echo "  --- Up-to-date Binary Update ---"

CURRENT_BIN="$(mktemp -t sshgo-current-XXXXX)"
if command -v go &>/dev/null; then
    echo "  Building test binary with version v0.0.1..."
    (cd "$PROJECT_DIR" && GOPROXY=off GONOSUMCHECK='*' GONOSUMDB='*' go build -ldflags="-X main.version=v0.0.1" -o "$CURRENT_BIN" . 2>/dev/null) && \
        echo -e "  ${GREEN}✓ Built test binary v0.0.1${NC}" || \
        echo -e "  ${YELLOW}⚠ go build failed, skipping${NC}"
    
    if [ -x "$CURRENT_BIN" ] && "$CURRENT_BIN" update --help 2>/dev/null | grep -q "sshgo update"; then
        OUTPUT=$(SSHGO_UPDATE_SERVER="$MOCK_URL" "$CURRENT_BIN" update 2>&1 || true)
        assert_contains "update (current): already up to date" "Already up to date (v0.0.1)" "$OUTPUT"
        assert_not_contains "update (current): no download" "Downloading" "$OUTPUT"
    fi
fi

# ------------------------------------------------------------------
# 0f: Error handling - unreachable server (use original binary)
# ------------------------------------------------------------------
echo "  --- Error Handling ---"

OUTPUT=$(SSHGO_UPDATE_SERVER="http://127.0.0.1:1" "$ORIG_BIN" update 2>&1 || true)
assert_contains "update: unreachable server error" "Failed to check for updates" "$OUTPUT"

# ------------------------------------------------------------------
# 0g: Unknown flag error (use original binary)
# ------------------------------------------------------------------
echo "  --- Unknown Flag Error ---"

OUTPUT=$("$ORIG_BIN" update --foo 2>&1 || true)
assert_contains "update: unknown flag error" "unknown flag" "$OUTPUT"
assert_contains "update: unknown flag shows usage" "Usage: sshgo update" "$OUTPUT"

# ------------------------------------------------------------------
# 0h: Mock server redirect with trailing slash
# ------------------------------------------------------------------
echo "  --- Redirect Edge Cases ---"

MOCK_SERVER_SCRIPT2="$(mktemp -t sshgo-mock-server2-XXXXX)"
cat > "$MOCK_SERVER_SCRIPT2" <<'PYEOF'
import sys
from http.server import HTTPServer, BaseHTTPRequestHandler

PORT = int(sys.argv[1])
BIN_PATH = sys.argv[2]
ARTIFACT = sys.argv[3]

class MockHandler2(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == '/releases/latest':
            self.send_response(302)
            self.send_header('Location', '/releases/tag/v0.0.2/')  # trailing slash
            self.end_headers()
        elif ARTIFACT in self.path and '/releases/download/' in self.path:
            self.send_response(200)
            self.send_header('Content-Type', 'application/octet-stream')
            self.end_headers()
            with open(BIN_PATH, 'rb') as f:
                self.wfile.write(f.read())
        else:
            self.send_response(404)
            self.end_headers()
    def log_message(self, format, *args):
        pass

HTTPServer(('127.0.0.1', PORT), MockHandler2).serve_forever()
PYEOF

MOCK_PORT2=18941
python3 "$MOCK_SERVER_SCRIPT2" "$MOCK_PORT2" "$MOCK_BIN" "$ARTIFACT_NAME" &
MOCK_SERVER_PID2=$!
sleep 1
MOCK_URL2="http://127.0.0.1:${MOCK_PORT2}"

# Use a fresh copy of the original binary
TRAIL_BIN="$(mktemp -t sshgo-trail-XXXXX)"
cp "$ORIG_BIN" "$TRAIL_BIN"
OUTPUT=$(SSHGO_UPDATE_SERVER="$MOCK_URL2" "$TRAIL_BIN" update 2>&1 || true)
assert_contains "update: trailing slash redirect" "v0.0.2" "$OUTPUT"

kill "$MOCK_SERVER_PID2" 2>/dev/null || true
rm -f "$MOCK_SERVER_SCRIPT2" "$TRAIL_BIN"

# ------------------------------------------------------------------
# 0i: Cleanup mock server and temp files
# ------------------------------------------------------------------
echo "  --- Cleanup ---"

kill "$MOCK_SERVER_PID" 2>/dev/null || true
rm -f "$MOCK_BIN" "$MOCK_SERVER_SCRIPT" "$OLD_BIN" "$CURRENT_BIN" "$DEV_BIN" "$ORIG_BIN" 2>/dev/null || true

echo -e "  ${GREEN}✓ Update test cleanup complete${NC}"
echo ""

enable_strict

# ============================================================================
# PHASE 1: Docker Setup
# ============================================================================
echo -e "${YELLOW}[Phase 1] Setting up Docker test environment${NC}"

NET_NAME="sshgo-test-net"
CONTAINER_NAME="sshgo-test-server"
SSH_PORT="2222"
TEST_HOST="e2e-testhost"
TEST_DIRECT="testuser@127.0.0.1:${SSH_PORT}"

# Clean up any leftovers
docker rm -f "$CONTAINER_NAME" 2>/dev/null || true
docker network rm "$NET_NAME" 2>/dev/null || true

# Build image
echo "  Building test SSH server image..."
docker build -t sshgo-test-server "$SCRIPT_DIR" 2>&1 | sed 's/^/  /'

# Start container (override caddy entrypoint)
echo "  Starting SSH server..."
docker run -d --name "$CONTAINER_NAME" \
    --entrypoint /usr/sbin/sshd \
    -p "$SSH_PORT:22" \
    sshgo-test-server -D -e 2>&1 | sed 's/^/  /'

# Wait for SSH to be ready
echo "  Waiting for SSH to start..."
for i in $(seq 1 20); do
    if docker logs "$CONTAINER_NAME" 2>&1 | grep -q "Server listening on"; then
        echo "  SSH ready (attempt $i)"
        break
    fi
    sleep 0.5
done

echo "  Using localhost with mapped port $SSH_PORT"

# Generate SSH key for testing
rm -f /tmp/sshgo_test_ed25519 /tmp/sshgo_test_ed25519.pub
echo "  Generating test SSH key..."
ssh-keygen -t ed25519 -f /tmp/sshgo_test_ed25519 -N "" -q 2>&1 | sed 's/^/  /'

# Deploy public key to container via stdin pipe
echo "  Deploying public key..."
cat /tmp/sshgo_test_ed25519.pub | docker exec -i "$CONTAINER_NAME" sh -c 'cat >> /home/testuser/.ssh/authorized_keys'
cat /tmp/sshgo_test_ed25519.pub | docker exec -i "$CONTAINER_NAME" sh -c 'cat >> /root/.ssh/authorized_keys'
docker exec "$CONTAINER_NAME" chown testuser:testuser /home/testuser/.ssh/authorized_keys
docker exec "$CONTAINER_NAME" chmod 600 /home/testuser/.ssh/authorized_keys /root/.ssh/authorized_keys

# Add test key to SSH agent for direct connections
ssh-add /tmp/sshgo_test_ed25519 2>&1 | sed 's/^/  /' || true

# Quick SSH connectivity test
echo "  Testing SSH connection..."
SSH_OK=$(ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
    -p "$SSH_PORT" -i /tmp/sshgo_test_ed25519 \
    testuser@127.0.0.1 "echo ready" 2>/dev/null)
if [ "$SSH_OK" = "ready" ]; then
    echo -e "  ${GREEN}✓ SSH connection OK${NC}"
else
    echo -e "  ${RED}✗ SSH connection failed${NC}"
    docker logs "$CONTAINER_NAME" 2>&1 | tail -5
    exit 1
fi

# Add host to ~/.ssh/config (save/restore original)
SSH_CONFIG_BAK="/tmp/sshgo_test_ssh_config_bak"
touch ~/.ssh/config
cp ~/.ssh/config "$SSH_CONFIG_BAK"

cat >> ~/.ssh/config <<CONFEOF

# sshgo E2E test entry (managed by test/setup.sh)
Host $TEST_HOST
  HostName 127.0.0.1
  Port $SSH_PORT
  User testuser
  IdentityFile /tmp/sshgo_test_ed25519
  StrictHostKeyChecking no
  UserKnownHostsFile /dev/null

Host ${TEST_HOST}-root
  HostName 127.0.0.1
  Port $SSH_PORT
  User root
  IdentityFile /tmp/sshgo_test_ed25519
  StrictHostKeyChecking no
  UserKnownHostsFile /dev/null
CONFEOF

# Import hosts from SSH config into local config (import-based architecture)
echo "  Importing hosts into local config..."
$SSHGO config import 2>&1 | sed 's/^/  /'

echo -e "  ${GREEN}✓ Docker test environment ready${NC}"
echo ""

# ============================================================================
# PHASE 2: Test Cases
# ============================================================================
disable_strict

echo -e "${YELLOW}[Phase 2] Running tests${NC}"
echo ""

# ------------------------------------------------------------------
# 2a: Legacy mode tests
# ------------------------------------------------------------------
echo "  --- Legacy Mode ---"

# 2a-1: Legacy command execution (standard output passthrough)
OUTPUT=$($SSHGO $TEST_HOST "echo hello_legacy" 2>/dev/null || true)
assert_eq "legacy: execute remote command" "hello_legacy" "$OUTPUT"

# 2a-2: Legacy command with exit code propagation
$SSHGO $TEST_HOST "exit 42" 2>/dev/null
RC=$?
assert_eq "legacy: exit code propagated" 42 "$RC"

# 2a-3: Legacy with direct host:port
OUTPUT=$($SSHGO $TEST_DIRECT "echo direct_ok" 2>/dev/null || true)
assert_eq "legacy: direct user@host:port" "direct_ok" "$OUTPUT"

# 2a-4: Interactive selection shows hosts
OUTPUT=$($SSHGO 2>&1 || true)
assert_contains "legacy: interactive shows host list" "$TEST_HOST" "$OUTPUT"

# 2a-5: Legacy sshgo <host> with command execution via direct address (proves non-config path works)
OUTPUT=$($SSHGO $TEST_DIRECT "echo direct_ok" 2>/dev/null || true)
assert_eq "legacy: direct user@host:port with command" "direct_ok" "$OUTPUT"

# ------------------------------------------------------------------
# 2b: Exec subcommand tests
# ------------------------------------------------------------------
echo "  --- Exec Subcommand ---"

# 2b-1: Exec basic JSON output
OUTPUT=$($SSHGO exec $TEST_HOST "echo hello_json" 2>/dev/null || true)
assert_contains "exec: JSON stdout field" "hello_json" "$OUTPUT"
assert_contains "exec: JSON contains exit_code" '"exit_code": 0' "$OUTPUT"
assert_contains "exec: JSON contains command" "echo hello_json" "$OUTPUT"

# 2b-2: Exec with non-zero exit code
OUTPUT=$($SSHGO exec $TEST_HOST "exit 7" 2>/dev/null || true)
assert_contains "exec: non-zero exit code" '"exit_code": 7' "$OUTPUT"

# 2b-3: Exec with stderr
OUTPUT=$($SSHGO exec $TEST_HOST "echo err_output >&2" 2>/dev/null || true)
assert_contains "exec: captures stderr" "err_output" "$OUTPUT"

# 2b-4: Exec --raw passthrough
OUTPUT=$($SSHGO exec --raw $TEST_HOST "echo raw_output" 2>/dev/null || true)
assert_eq "exec --raw: raw stdout passthrough" "raw_output" "$OUTPUT"

# 2b-5: Exec --raw exit code propagated
$SSHGO exec --raw $TEST_HOST "exit 99" 2>/dev/null
RC=$?
assert_eq "exec --raw: exit code propagated" 99 "$RC"

# 2b-6: Exec with sudo (NOPASSWD for /usr/bin/whoami)
OUTPUT=$($SSHGO exec --sudo $TEST_HOST "/usr/bin/whoami" 2>/dev/null || true)
assert_contains "exec --sudo: structured JSON output" "exit_code" "$OUTPUT"
echo "$OUTPUT" | python3 -m json.tool > /dev/null 2>&1 && pass "exec --sudo: valid JSON" || fail "exec --sudo: invalid JSON"
assert_contains "exec --sudo: ran as root (via NOPASSWD)" "root" "$OUTPUT"

# 2b-7: Exec with complex command (pipe)
OUTPUT=$($SSHGO exec $TEST_HOST "echo 'a b c' | wc -w" 2>/dev/null || true)
assert_contains "exec: piped command result" '"stdout": "3' "$OUTPUT"

# 2b-8: Exec with large output (10k chars)
OUTPUT=$($SSHGO exec $TEST_HOST "dd if=/dev/zero bs=1 count=10000 2>/dev/null | tr '\0' 'x'" 2>/dev/null || true)
assert_contains "exec: large output (10k chars)" '"stdout": "x' "$OUTPUT"

# 2b-9: Exec duration field present
OUTPUT=$($SSHGO exec $TEST_HOST "echo check_duration" 2>/dev/null || true)
assert_contains "exec: contains duration field" '"duration"' "$OUTPUT"

# 2b-10: Exec JSON is valid
OUTPUT=$($SSHGO exec $TEST_HOST "echo valid_json_check" 2>/dev/null || true)
echo "$OUTPUT" | python3 -m json.tool > /dev/null 2>&1 && pass "exec: valid JSON structure" || fail "exec: invalid JSON structure"

# ------------------------------------------------------------------
# 2c: Info subcommand tests
# ------------------------------------------------------------------
echo "  --- Info Subcommand ---"

# 2c-1: Info human-readable
OUTPUT=$($SSHGO info $TEST_HOST 2>/dev/null || true)
assert_contains "info: shows hostname" "127.0.0.1" "$OUTPUT"
assert_contains "info: shows user" "testuser" "$OUTPUT"
assert_contains "info: shows source" "imported" "$OUTPUT"
assert_contains "info: shows auth methods" "Auth" "$OUTPUT"

# 2c-2: Info JSON output
OUTPUT=$($SSHGO info --json $TEST_HOST 2>/dev/null || true)
assert_contains "info --json: hostname field" "127.0.0.1" "$OUTPUT"
assert_contains "info --json: user field" '"user": "testuser"' "$OUTPUT"
assert_contains "info --json: source field" '"source": "imported"' "$OUTPUT"
assert_contains "info --json: auth methods field" "auth_methods" "$OUTPUT"

# 2c-3: Info does NOT expose credentials (auth method names are OK, key paths are not)
OUTPUT=$($SSHGO info --json $TEST_HOST 2>/dev/null || true)
assert_not_contains "info: no password exposure" "password" "$OUTPUT"
# 'IdentityFile' as a method name is fine; actual paths should NOT appear
assert_not_contains "info: no key path exposure" "/tmp/sshgo_test" "$OUTPUT"

# 2c-4: Info for direct host (user@host:port)
OUTPUT=$($SSHGO info $TEST_DIRECT 2>/dev/null || true)
assert_contains "info: direct host shows port" "2222" "$OUTPUT"
assert_contains "info: direct host shows source" "direct" "$OUTPUT"

# 2c-5: Info JSON is valid
OUTPUT=$($SSHGO info --json $TEST_HOST 2>/dev/null || true)
echo "$OUTPUT" | python3 -m json.tool > /dev/null 2>&1 && pass "info --json: valid JSON" || fail "info --json: invalid JSON"

# ------------------------------------------------------------------
# 2d: Config subcommand tests
# ------------------------------------------------------------------
echo "  --- Config Subcommand ---"

# Clean up any previous config
rm -f ~/.sshgo_config

# 2d-1: Config set alias
OUTPUT=$($SSHGO config set $TEST_HOST alias myalias 2>&1 || true)
assert_contains "config set: alias accepted" "$TEST_HOST" "$OUTPUT"

# 2d-2: Config set tags
OUTPUT=$($SSHGO config set $TEST_HOST tags e2e,test,docker 2>&1 || true)
assert_contains "config set: tags accepted" "$TEST_HOST" "$OUTPUT"

# 2d-3: Config set notes
OUTPUT=$($SSHGO config set $TEST_HOST notes "E2E test host" 2>&1 || true)
assert_contains "config set: notes accepted" "$TEST_HOST" "$OUTPUT"

# 2d-4: Config set priority
OUTPUT=$($SSHGO config set $TEST_HOST priority agent,key,password 2>&1 || true)
assert_contains "config set: priority accepted" "$TEST_HOST" "$OUTPUT"

# 2d-5: Config list
OUTPUT=$($SSHGO config list 2>&1 || true)
assert_contains "config list: host listed" "$TEST_HOST" "$OUTPUT"
assert_contains "config list: shows alias" "myalias" "$OUTPUT"
assert_contains "config list: shows tags" "docker" "$OUTPUT"
assert_contains "config list: shows notes" "E2E test host" "$OUTPUT"

# 2d-6: Config get all (no key specified)
OUTPUT=$($SSHGO config get $TEST_HOST 2>&1 || true)
assert_contains "config get all: shows alias" "myalias" "$OUTPUT"
assert_contains "config get all: shows tags" "e2e" "$OUTPUT"

# 2d-7: Config get specific keys
OUTPUT=$($SSHGO config get $TEST_HOST alias 2>&1 || true)
assert_eq "config get: alias" "myalias" "$OUTPUT"

OUTPUT=$($SSHGO config get $TEST_HOST tags 2>&1 || true)
assert_contains "config get: tags" "e2e" "$OUTPUT"

OUTPUT=$($SSHGO config get $TEST_HOST notes 2>&1 || true)
assert_eq "config get: notes" "E2E test host" "$OUTPUT"

OUTPUT=$($SSHGO config get $TEST_HOST priority 2>&1 || true)
assert_eq "config get: priority" "agent, key, password" "$OUTPUT"

# 2d-8: Config find by tag
OUTPUT=$($SSHGO config find --tag e2e 2>&1 || true)
assert_contains "config find: tag search hit" "$TEST_HOST" "$OUTPUT"

OUTPUT=$($SSHGO config find --tag nonexistent 2>&1 || true)
assert_contains "config find: tag search miss" "nonexistent" "$OUTPUT"

# 2d-9: Config unset specific key
$SSHGO config unset $TEST_HOST notes 2>&1
OUTPUT=$($SSHGO config get $TEST_HOST notes 2>&1 || true)
assert_eq "config unset: key value cleared" "" "$OUTPUT"

# 2d-10: Config unset entire host
$SSHGO config unset $TEST_HOST 2>&1
OUTPUT=$($SSHGO config list 2>&1 || true)
assert_not_contains "config unset: host entry removed" "$TEST_HOST" "$OUTPUT"

# ------------------------------------------------------------------
# 2e: Alias resolution tests
# ------------------------------------------------------------------
echo "  --- Alias Resolution ---"

# Re-add host with alias and connection details for alias resolution testing
$SSHGO config set $TEST_HOST alias myalias 2>&1
$SSHGO config set $TEST_HOST hostname 127.0.0.1 2>&1
$SSHGO config set $TEST_HOST port 2222 2>&1
$SSHGO config set $TEST_HOST user testuser 2>&1
$SSHGO config set $TEST_HOST identity-file /tmp/sshgo_test_ed25519 2>&1

# 2e-1: Exec using alias
OUTPUT=$($SSHGO exec myalias "echo alias_works" 2>/dev/null || true)
assert_contains "exec by alias: resolves target" "alias_works" "$OUTPUT"

# 2e-2: Info using alias
OUTPUT=$($SSHGO info myalias 2>/dev/null || true)
assert_contains "info by alias: resolves to host" "testuser" "$OUTPUT"

# 2e-3: Legacy mode using alias
OUTPUT=$($SSHGO myalias "echo legacy_alias" 2>/dev/null || true)
assert_eq "legacy by alias: works" "legacy_alias" "$OUTPUT"

# 2e-4: Info JSON shows metadata when alias is set
OUTPUT=$($SSHGO info --json $TEST_HOST 2>/dev/null || true)
assert_contains "info: includes meta block when alias set" '"meta"' "$OUTPUT"

# ------------------------------------------------------------------
# 2f: Error handling tests
# ------------------------------------------------------------------
echo "  --- Error Handling ---"

# 2f-1: Exec on unreachable host (gets treated as direct, fails on connection)
OUTPUT=$($SSHGO exec 192.0.2.1 "echo test" 2>&1 || true)
assert_contains "exec: unreachable host error" "Connection failed" "$OUTPUT"

# 2f-2: Info on unknown host (bare hostname is treated as direct connection)
OUTPUT=$($SSHGO info nonexistent 2>&1 || true)
assert_contains "info: unknown host shows as direct" "direct" "$OUTPUT"

# 2f-3: Config invalid key
OUTPUT=$($SSHGO config set $TEST_HOST invalid_key value 2>&1 || true)
assert_contains "config set: invalid key error" "unknown key" "$OUTPUT"

# 2f-4: Exec missing arguments
OUTPUT=$($SSHGO exec 2>&1 || true)
assert_contains "exec: missing args error" "missing host or command" "$OUTPUT"

# 2f-5: Info missing arguments
OUTPUT=$($SSHGO info 2>&1 || true)
assert_contains "info: missing args error" "Usage: sshgo info" "$OUTPUT"

# ------------------------------------------------------------------
# 2g: Edge cases
# ------------------------------------------------------------------
echo "  --- Edge Cases ---"

# 2g-1: Exec command with special characters
OUTPUT=$($SSHGO exec $TEST_HOST "echo 'special chars: !@#$'" 2>/dev/null || true)
assert_contains "exec: special characters preserved" "special chars:" "$OUTPUT"

# 2g-2: Exec with empty stdout (true command)
OUTPUT=$($SSHGO exec $TEST_HOST "true" 2>/dev/null || true)
assert_contains "exec: empty stdout" '"stdout": ""' "$OUTPUT"

# 2g-3: Exec non-existing command
OUTPUT=$($SSHGO exec $TEST_HOST "nonexistent_cmd_xyz" 2>/dev/null || true)
assert_contains "exec: command not found error" "exit_code" "$OUTPUT"

# 2g-4: Config multiple hosts
$SSHGO config set host-a alias ha 2>&1
$SSHGO config set host-b tags test 2>&1
OUTPUT=$($SSHGO config list 2>&1 || true)
assert_contains "config: multiple hosts: host-a" "host-a" "$OUTPUT"
assert_contains "config: multiple hosts: host-b" "host-b" "$OUTPUT"

# Clean up extra test hosts
$SSHGO config unset host-a 2>&1
$SSHGO config unset host-b 2>&1

# 2g-5: Config JSON file validity
python3 -m json.tool ~/.sshgo_config > /dev/null 2>&1 && pass "config: ~/.sshgo_config is valid JSON" || fail "config: ~/.sshgo_config is invalid JSON"

# 2g-6: Help subcommands print usage
OUTPUT=$($SSHGO exec --help 2>&1 || true)
assert_contains "exec --help: shows usage" "Usage: sshgo exec" "$OUTPUT"
OUTPUT=$($SSHGO info --help 2>&1 || true)
assert_contains "info --help: shows usage" "Usage: sshgo info" "$OUTPUT"
OUTPUT=$($SSHGO config --help 2>&1 || true)
assert_contains "config --help: shows usage" "Usage: sshgo config" "$OUTPUT"

# 2g-7: Main --help
OUTPUT=$($SSHGO --help 2>&1 || true)
assert_contains "main --help: shows subcommands" "exec" "$OUTPUT"
assert_contains "main --help: shows subcommands" "info" "$OUTPUT"
assert_contains "main --help: shows subcommands" "config" "$OUTPUT"

# 2g-8: Config find by tag (re-set tags because 2d-10 unset the host)
$SSHGO config set $TEST_HOST tags e2e,test,docker 2>&1
OUTPUT=$($SSHGO config find --tag docker 2>&1 || true)
assert_contains "config find: tag=docker finds host" "$TEST_HOST" "$OUTPUT"

# ------------------------------------------------------------------
# 2h: Sudo password tests
# ------------------------------------------------------------------
echo "  --- Sudo Password ---"

# 2h-1: Config help shows set-sudo-password command
OUTPUT=$($SSHGO config --help 2>&1 || true)
assert_contains "config help: shows set-sudo-password" "set-sudo-password" "$OUTPUT"

# 2h-2: Exec --sudo with non-NOPASSWD command fails (no TTY for sudo password)
OUTPUT=$($SSHGO exec --sudo $TEST_HOST "/bin/hostname" 2>/dev/null || true)
assert_not_contains "exec --sudo: non-NOPASSWD command requires password" '"exit_code": 0' "$OUTPUT"

# 2h-3: Stderr hint about set-sudo-password when no sudo password saved
STDERR=$($SSHGO exec --sudo $TEST_HOST "/bin/hostname" 2>&1 1>/dev/null || true)
assert_contains "exec --sudo: stderr hint about set-sudo-password" "set-sudo-password" "$STDERR"

enable_strict

# ============================================================================
# PHASE 3: Summary
# ============================================================================
echo ""
echo -e "${CYAN}========================================${NC}"
echo -e "${CYAN}  Test Results${NC}"
echo -e "${CYAN}========================================${NC}"
echo -e "  ${GREEN}PASS: $PASS${NC}"
echo -e "  ${RED}FAIL: $FAIL${NC}"
echo -e "  Total: $((PASS + FAIL))"
echo ""

# Record test results to file
RESULTS_FILE="$SCRIPT_DIR/results.txt"
{
    echo "sshgo E2E Test Results"
    echo "Date: $(date)"
    echo "================================"
    echo "PASS: $PASS"
    echo "FAIL: $FAIL"
    echo "Total: $((PASS + FAIL))"
    echo ""
    echo "Test Details:"
    for t in "${TESTS[@]}"; do
        echo "  $t"
    done
} > "$RESULTS_FILE"
echo "  Results saved to: $RESULTS_FILE"
echo ""

# ============================================================================
# PHASE 4: Cleanup
# ============================================================================
echo -e "${YELLOW}[Phase 4] Cleaning up${NC}"

# Restore SSH config
if [ -f "$SSH_CONFIG_BAK" ]; then
    cp "$SSH_CONFIG_BAK" ~/.ssh/config
    rm -f "$SSH_CONFIG_BAK"
fi

# Clean up test data
rm -f ~/.sshgo_config
rm -f /tmp/sshgo_test_ed25519 /tmp/sshgo_test_ed25519.pub

# Stop and remove Docker container and image
docker rm -f "$CONTAINER_NAME" 2>&1 | sed 's/^/  /'
docker rmi sshgo-test-server 2>&1 | sed 's/^/  /'

echo -e "  ${GREEN}✓ Cleanup complete${NC}"
echo ""

# Exit with appropriate code
if [ "$FAIL" -gt 0 ]; then
    echo -e "${RED}Some tests failed!${NC}"
    echo ""
    echo "Failed tests:"
    for t in "${TESTS[@]}"; do
        if [[ "$t" == FAIL:* ]]; then
            echo "  - ${t#FAIL: }"
        fi
    done
    exit 1
else
    echo -e "${GREEN}All tests passed!${NC}"
    exit 0
fi
