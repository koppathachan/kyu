#!/usr/bin/env sh
#
# Tests for next-version.sh. Each case builds a throwaway repository, makes
# commits and tags, and checks the tag the script prints.

set -eu

script=$(cd "$(dirname "$0")" && pwd)/next-version.sh
failures=0

# fresh starts an empty repository in a temporary directory and enters it.
fresh() {
	dir=$(mktemp -d)
	cd "$dir"
	git init -q
	git config user.name test
	git config user.email test@example.com
	git config commit.gpgsign false
	git config tag.gpgsign false
}

commit() {
	git commit -q --allow-empty -m "$1"
}

expect() {
	got=$("$script")
	if [ "$got" = "$1" ]; then
		echo "ok    $2"
	else
		echo "FAIL  $2: want '$1', got '$got'"
		failures=$((failures + 1))
	fi
	rm -rf "$dir"
	cd - >/dev/null
}

fresh
commit "docs: write the README"
commit "ci: add checks"
expect "" "no release when nothing calls for one"

fresh
commit "docs: write the README"
commit "feat: build the queue engine"
expect "v0.1.0" "the first release is v0.1.0"

fresh
commit "fix: something"
expect "v0.1.0" "the first release is v0.1.0 whatever the bump"

fresh
commit "feat: a"
git tag v0.1.0
commit "fix: b"
expect "v0.1.1" "fix bumps the patch"

fresh
commit "feat: a"
git tag v0.1.0
commit "perf(engine): b"
expect "v0.1.1" "perf with a scope bumps the patch"

fresh
commit "feat: a"
git tag v0.1.0
commit "fix: b"
commit "feat(queue): c"
commit "docs: d"
expect "v0.2.0" "the largest bump since the last release wins"

fresh
commit "feat: a"
git tag v0.4.2
commit "feat!: drop the old API"
expect "v0.5.0" "before 1.0, a breaking change bumps the minor"

fresh
commit "feat: a"
git tag v1.4.2
commit "feat(api)!: drop the old API"
expect "v2.0.0" "from 1.0, a breaking subject bumps the major"

fresh
commit "feat: a"
git tag v1.4.2
commit "fix: b

BREAKING CHANGE: the field is renamed"
expect "v2.0.0" "a BREAKING CHANGE footer bumps the major"

fresh
commit "feat: a"
git tag v1.4.2
commit "build: move the module (#1)

* build: move the module

Breaking-Change: the import path moves"
expect "v2.0.0" "a breaking footer quoted in a squash merge counts"

fresh
commit "feat: a"
git tag v1.0.0
commit "docs: mention BREAKING CHANGE: in passing"
expect "" "a breaking marker in a subject's prose does not count"

fresh
commit "feat: a"
git tag v0.1.0
commit "feat: b"
git tag v0.2.0-rc.1
commit "fix: c"
expect "v0.2.0" "a prerelease tag is not the last release"

fresh
commit "feat: a"
git tag v0.1.0
expect "" "a tagged HEAD needs no release"

fresh
commit "feature: not a type"
commit "Fix: not lower case"
expect "" "an unknown or miscased type releases nothing"

fresh
commit "feat: a"
git tag v0.9.0
commit "feat: b"
git tag v0.10.0
commit "fix: c"
expect "v0.10.1" "versions compare as numbers, not text"

if [ "$failures" -gt 0 ]; then
	echo "$failures failed"
	exit 1
fi
