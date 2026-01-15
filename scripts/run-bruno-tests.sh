#!/bin/bash
# Bruno API test runner for go-restgen examples
# Usage: ./scripts/run-bruno-tests.sh [simple|nested|auth|validator|audit|uuid|custom|relations|files-proxy|files-signed|actions|batch|all]

set -e

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Default to running all tests
TEST_SUITE="${1:-all}"

print_header() {
    echo -e "${BLUE}================================================${NC}"
    echo -e "${BLUE}$1${NC}"
    echo -e "${BLUE}================================================${NC}"
}

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

print_error() {
    echo -e "${RED}✗ $1${NC}"
}

print_info() {
    echo -e "${YELLOW}ℹ $1${NC}"
}

# Check if Bruno CLI is available
check_bruno() {
    if ! command -v bru &> /dev/null && ! npx @usebruno/cli --version &> /dev/null 2>&1; then
        print_error "Bruno CLI not found. Install with: npm install -g @usebruno/cli"
        exit 1
    fi
}

# Run Bruno CLI (prefer global, fallback to npx)
run_bruno() {
    if command -v bru &> /dev/null; then
        bru "$@"
    else
        npx @usebruno/cli "$@"
    fi
}

# Start server and wait for it to be ready
start_server() {
    local example_dir="$1"
    local port="${2:-8080}"

    # Check if port is already in use
    if lsof -ti :"$port" > /dev/null 2>&1; then
        print_error "Port $port is already in use. Please stop the existing server first."
        exit 1
    fi

    print_info "Starting server from $example_dir..."

    cd "$PROJECT_ROOT/$example_dir"
    go run . &
    SERVER_PID=$!
    SERVER_PORT=$port

    # Wait for server to be ready (max 10 seconds)
    local max_attempts=20
    local attempt=0
    while ! curl -s "http://localhost:$port/health" > /dev/null 2>&1; do
        sleep 0.5
        attempt=$((attempt + 1))
        if [ $attempt -ge $max_attempts ]; then
            print_error "Server failed to start within 10 seconds"
            kill $SERVER_PID 2>/dev/null || true
            exit 1
        fi
    done

    print_success "Server started (PID: $SERVER_PID)"
}

# Stop the server
stop_server() {
    if [ -n "$SERVER_PID" ]; then
        print_info "Stopping server (PID: $SERVER_PID)..."
        # Kill go run process
        kill $SERVER_PID 2>/dev/null || true
        # Also kill any process listening on the port (the compiled binary)
        # go run spawns the actual server as a child process
        if [ -n "$SERVER_PORT" ]; then
            lsof -ti :"$SERVER_PORT" | xargs kill 2>/dev/null || true
        fi
        wait $SERVER_PID 2>/dev/null || true
        print_success "Server stopped"
    fi
}

# Cleanup on exit
cleanup() {
    stop_server
}
trap cleanup EXIT

# Run tests for a specific example
run_tests() {
    local name="$1"
    local example_dir="$2"
    local bruno_dir="$3"
    local port="${4:-8080}"

    print_header "Running $name Tests"

    start_server "$example_dir" "$port"

    cd "$PROJECT_ROOT/bruno"
    if run_bruno run "$bruno_dir" --env local --format json; then
        print_success "$name tests passed"
        RESULT=0
    else
        print_error "$name tests failed"
        RESULT=1
    fi

    stop_server
    SERVER_PID=""
    SERVER_PORT=""

    return $RESULT
}

# Main execution
check_bruno

FAILED=0

case "$TEST_SUITE" in
    simple)
        run_tests "Simple Example" "examples/simple" "simple-example" || FAILED=1
        ;;
    nested)
        run_tests "Nested Example" "examples/nested_routes" "nested-example" || FAILED=1
        ;;
    auth)
        run_tests "Auth Example" "examples/auth" "auth-example" || FAILED=1
        ;;
    validator)
        run_tests "Validator Example" "examples/validator" "validator-example" || FAILED=1
        ;;
    audit)
        run_tests "Audit Example" "examples/audit" "audit-example" || FAILED=1
        ;;
    uuid)
        run_tests "UUID Example" "examples/uuid_pk" "uuid-example" || FAILED=1
        ;;
    custom)
        run_tests "Custom Handlers Example" "examples/custom" "custom-example" || FAILED=1
        ;;
    relations)
        run_tests "Relations Example" "examples/relations" "relations-example" || FAILED=1
        ;;
    files-proxy)
        run_tests "Files Proxy Example" "examples/files_proxy" "files-proxy-example" || FAILED=1
        ;;
    files-signed)
        run_tests "Files Signed Example" "examples/files_signed" "files-signed-example" || FAILED=1
        ;;
    actions)
        run_tests "Actions Example" "examples/actions" "actions-example" || FAILED=1
        ;;
    batch)
        run_tests "Batch Example" "examples/batch" "batch-example" || FAILED=1
        ;;
    all)
        run_tests "Simple Example" "examples/simple" "simple-example" || FAILED=1
        run_tests "Nested Example" "examples/nested_routes" "nested-example" || FAILED=1
        run_tests "Auth Example" "examples/auth" "auth-example" || FAILED=1
        run_tests "Validator Example" "examples/validator" "validator-example" || FAILED=1
        run_tests "Audit Example" "examples/audit" "audit-example" || FAILED=1
        run_tests "UUID Example" "examples/uuid_pk" "uuid-example" || FAILED=1
        run_tests "Custom Handlers Example" "examples/custom" "custom-example" || FAILED=1
        run_tests "Relations Example" "examples/relations" "relations-example" || FAILED=1
        run_tests "Files Proxy Example" "examples/files_proxy" "files-proxy-example" || FAILED=1
        run_tests "Files Signed Example" "examples/files_signed" "files-signed-example" || FAILED=1
        run_tests "Actions Example" "examples/actions" "actions-example" || FAILED=1
        run_tests "Batch Example" "examples/batch" "batch-example" || FAILED=1
        ;;
    *)
        echo "Usage: $0 [simple|nested|auth|validator|audit|uuid|custom|relations|files-proxy|files-signed|actions|batch|all]"
        exit 1
        ;;
esac

echo ""
if [ $FAILED -eq 0 ]; then
    print_header "All Tests Passed"
else
    print_header "Some Tests Failed"
    exit 1
fi
