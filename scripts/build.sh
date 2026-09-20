#!/usr/bin/env sh
#
# Build every kyu binary into bin/.
#
# Building needs Go alone: the generated code under kyu/ is committed, so there
# is no code generation step before a build and no need for buf.

set -eu

out=${BIN_DIR:-bin}
mkdir -p "$out"

for main in cmd/*/; do
	name=$(basename "$main")
	echo "building $name"
	go build -o "$out/$name" "./$main"
done

echo "binaries in $out/"
