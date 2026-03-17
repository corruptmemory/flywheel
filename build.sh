#!/usr/bin/env bash
set -euo pipefail

BINARY="flywheel"

usage() {
    cat <<USAGE
Usage: $(basename "$0") [OPTIONS]

Options:
  -b, --build    Build the binary
  -t, --test     Run tests
  -r, --race     Run tests with race detector
  -V, --vet      Run go vet
  -c, --clean    Remove built binary
  -a, --all      Vet + test + build
  -h, --help     Show this help
USAGE
}

do_vet=false
do_test=false
do_race=false
do_build=false
do_clean=false

if [[ $# -eq 0 ]]; then
    usage
    exit 0
fi

while [[ $# -gt 0 ]]; do
    case "$1" in
        -b|--build) do_build=true ;;
        -t|--test)  do_test=true ;;
        -r|--race)  do_race=true ;;
        -V|--vet)   do_vet=true ;;
        -c|--clean) do_clean=true ;;
        -a|--all)   do_vet=true; do_test=true; do_build=true ;;
        -h|--help)  usage; exit 0 ;;
        *) echo "Unknown option: $1"; usage; exit 1 ;;
    esac
    shift
done

if $do_clean; then
    echo "==> Cleaning..."
    rm -f "$BINARY"
    echo "    Done."
fi

if $do_vet; then
    echo "==> Vetting..."
    go vet ./...
    echo "    Done."
fi

if $do_test; then
    echo "==> Testing..."
    go test ./...
    echo "    Done."
fi

if $do_race; then
    echo "==> Testing (race detector)..."
    go test -race ./...
    echo "    Done."
fi

if $do_build; then
    echo "==> Building..."
    CGO_ENABLED=0 go build -trimpath -o "$BINARY" ./cmd/flywheel/
    echo "    Done. Built ./$BINARY"
fi
