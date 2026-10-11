#!/usr/bin/env sh
#
# Write THIRD_PARTY_NOTICES: the license and notice files of everything
# compiled into kyu's binaries.
#
# kyu is MIT, but its binaries statically link code under other licenses. Go's
# runtime and standard library, protobuf and golang.org/x/... are BSD-3-Clause,
# which requires a binary copy to reproduce their copyright notices. gRPC and
# genproto are Apache-2.0, which requires a copy of the license and of any
# NOTICE file. Every archive, package and image ships this file beside LICENSE.
#
# The set of modules comes from the import graph of ./cmd/..., so a module that
# is only a tool or a test dependency is not listed, and one that becomes part
# of a binary cannot be missed. `make notices-check`, which CI runs, fails if
# the committed file is stale.

set -eu

out=${1:-THIRD_PARTY_NOTICES}

# Every non-standard module a binary links, as path@version.
modules=$(go list -deps -f '{{if not .Standard}}{{with .Module}}{{if not .Main}}{{.Path}}@{{.Version}}{{end}}{{end}}{{end}}' ./cmd/... | sort -u)

section() {
	# $1 heading, $2 directory holding the license files
	printf '%s\n' "================================================================================"
	printf '%s\n' "$1"
	printf '%s\n\n' "================================================================================"
	found=
	for f in "$2"/LICENSE* "$2"/LICENCE* "$2"/COPYING* "$2"/NOTICE*; do
		[ -f "$f" ] || continue
		found=1
		printf -- '--- %s ---\n\n' "$(basename "$f")"
		cat "$f"
		printf '\n'
	done
	if [ -z "$found" ]; then
		echo "third-party-notices: no license file found for $1 in $2" >&2
		exit 1
	fi
}

{
	cat <<'EOF'
kyu is distributed under the MIT License; see LICENSE.

Its binaries include the following third-party software, whose licenses and
notices are reproduced below as those licenses require. This file is generated
by scripts/third-party-notices.sh; do not edit it by hand.

EOF
	# The Go runtime and standard library are compiled into every binary. Their
	# license is reproduced here rather than read from GOROOT, because not
	# every Go installation ships the file — Homebrew's does not — and the
	# output must not depend on whose machine generated it. The text has not
	# changed across Go releases; it is the same text golang.org/x/... carry.
	printf '%s\n' "================================================================================"
	printf '%s\n' "The Go programming language (runtime and standard library)"
	printf '%s\n\n' "================================================================================"
	printf -- '--- LICENSE ---\n\n'
	cat <<'GO_LICENSE'
Copyright 2009 The Go Authors.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are
met:

   * Redistributions of source code must retain the above copyright
notice, this list of conditions and the following disclaimer.
   * Redistributions in binary form must reproduce the above
copyright notice, this list of conditions and the following disclaimer
in the documentation and/or other materials provided with the
distribution.
   * Neither the name of Google LLC nor the names of its
contributors may be used to endorse or promote products derived from
this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
"AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
GO_LICENSE
	printf '\n'

	for m in $modules; do
		dir=$(go mod download -json "$m" | sed -n 's/^[[:space:]]*"Dir": "\(.*\)",$/\1/p')
		section "${m%@*} ${m#*@}" "$dir"
	done
} >"$out"
