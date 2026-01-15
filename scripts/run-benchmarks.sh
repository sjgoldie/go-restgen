#!/bin/bash
# Performance benchmarking script for go-restgen
# Runs comprehensive benchmarks with CPU and memory profiling

set -e

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Default values
BENCH_TIME="${BENCH_TIME:-3s}"
BENCH_PATTERN="${BENCH_PATTERN:-.}"
OUTPUT_DIR="${OUTPUT_DIR:-./bench-results}"
ENABLE_CPU_PROF="${ENABLE_CPU_PROF:-true}"
ENABLE_MEM_PROF="${ENABLE_MEM_PROF:-true}"
ENABLE_TRACE="${ENABLE_TRACE:-false}"

print_header() {
    echo -e "${BLUE}================================================${NC}"
    echo -e "${BLUE}$1${NC}"
    echo -e "${BLUE}================================================${NC}"
}

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

print_info() {
    echo -e "${YELLOW}ℹ $1${NC}"
}

# Create output directory
mkdir -p "$OUTPUT_DIR"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)

print_header "go-restgen Performance Benchmarks"
echo ""
print_info "Benchmark time: $BENCH_TIME"
print_info "Pattern: $BENCH_PATTERN"
print_info "Output directory: $OUTPUT_DIR"
echo ""

# Run benchmarks with profiling
print_header "Running Benchmarks"

BENCH_FLAGS="-bench=$BENCH_PATTERN -benchtime=$BENCH_TIME -benchmem"

if [ "$ENABLE_CPU_PROF" = "true" ]; then
    BENCH_FLAGS="$BENCH_FLAGS -cpuprofile=$OUTPUT_DIR/cpu_${TIMESTAMP}.prof"
fi

if [ "$ENABLE_MEM_PROF" = "true" ]; then
    BENCH_FLAGS="$BENCH_FLAGS -memprofile=$OUTPUT_DIR/mem_${TIMESTAMP}.prof"
fi

if [ "$ENABLE_TRACE" = "true" ]; then
    BENCH_FLAGS="$BENCH_FLAGS -trace=$OUTPUT_DIR/trace_${TIMESTAMP}.out"
fi

# Run the benchmarks
cd "$(dirname "$0")/.."
echo "Running: go test -run=^$ $BENCH_FLAGS ./handler"
go test -run='^$' $BENCH_FLAGS ./handler | tee "$OUTPUT_DIR/bench_${TIMESTAMP}.txt"

print_success "Benchmarks completed"
echo ""

# Generate reports
print_header "Generating Profile Reports"

if [ "$ENABLE_CPU_PROF" = "true" ] && [ -f "$OUTPUT_DIR/cpu_${TIMESTAMP}.prof" ]; then
    print_info "CPU Profile saved to: $OUTPUT_DIR/cpu_${TIMESTAMP}.prof"
    echo "  View with: go tool pprof -http=:8080 $OUTPUT_DIR/cpu_${TIMESTAMP}.prof"

    # Generate text report
    go tool pprof -text "$OUTPUT_DIR/cpu_${TIMESTAMP}.prof" > "$OUTPUT_DIR/cpu_${TIMESTAMP}_report.txt"
    print_success "CPU text report: $OUTPUT_DIR/cpu_${TIMESTAMP}_report.txt"

    # Show top 10 functions
    echo ""
    print_info "Top 10 CPU consumers:"
    go tool pprof -top -cum "$OUTPUT_DIR/cpu_${TIMESTAMP}.prof" | head -20
fi

if [ "$ENABLE_MEM_PROF" = "true" ] && [ -f "$OUTPUT_DIR/mem_${TIMESTAMP}.prof" ]; then
    print_info "Memory Profile saved to: $OUTPUT_DIR/mem_${TIMESTAMP}.prof"
    echo "  View with: go tool pprof -http=:8080 $OUTPUT_DIR/mem_${TIMESTAMP}.prof"

    # Generate text report
    go tool pprof -text "$OUTPUT_DIR/mem_${TIMESTAMP}.prof" > "$OUTPUT_DIR/mem_${TIMESTAMP}_report.txt"
    print_success "Memory text report: $OUTPUT_DIR/mem_${TIMESTAMP}_report.txt"

    # Show top 10 allocators
    echo ""
    print_info "Top 10 memory allocators:"
    go tool pprof -top -alloc_space "$OUTPUT_DIR/mem_${TIMESTAMP}.prof" | head -20
fi

if [ "$ENABLE_TRACE" = "true" ] && [ -f "$OUTPUT_DIR/trace_${TIMESTAMP}.out" ]; then
    print_info "Trace saved to: $OUTPUT_DIR/trace_${TIMESTAMP}.out"
    echo "  View with: go tool trace $OUTPUT_DIR/trace_${TIMESTAMP}.out"
fi

echo ""
print_header "Summary"
echo "Results saved to: $OUTPUT_DIR"
echo ""
echo "Quick commands:"
echo "  - View CPU profile:    go tool pprof -http=:8080 $OUTPUT_DIR/cpu_${TIMESTAMP}.prof"
echo "  - View memory profile: go tool pprof -http=:8080 $OUTPUT_DIR/mem_${TIMESTAMP}.prof"
echo "  - Compare runs:        benchstat $OUTPUT_DIR/bench_old.txt $OUTPUT_DIR/bench_${TIMESTAMP}.txt"
if [ "$ENABLE_TRACE" = "true" ]; then
    echo "  - View trace:          go tool trace $OUTPUT_DIR/trace_${TIMESTAMP}.out"
fi
echo ""

print_success "Done!"
