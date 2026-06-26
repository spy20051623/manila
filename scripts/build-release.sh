#!/usr/bin/env sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH= cd -- "$script_dir/.." && pwd)
release_dir="$repo_root/bin/release"

cd "$repo_root"

rm -rf "$release_dir"
mkdir -p "$release_dir"

build_target() {
    goos="$1"
    goarch="$2"
    extension="$3"
    target_name="$goos-$goarch"
    out_dir="$release_dir/$target_name"
    out_file="$out_dir/manila-server$extension"

    mkdir -p "$out_dir"
    printf 'Building %s -> %s\n' "$target_name" "$out_file"
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
        go build -trimpath -ldflags "-s -w" -o "$out_file" ./cmd/server
}

build_target windows amd64 .exe
build_target windows arm64 .exe
build_target linux amd64 ""
build_target linux arm64 ""

printf 'Release binaries written to %s\n' "$release_dir"
