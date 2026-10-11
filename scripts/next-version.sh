#!/usr/bin/env sh
#
# Print the tag the next release of HEAD should carry, or nothing if the
# commits since the last release do not call for one.
#
# The version follows from commit messages, written as Conventional Commits
# (https://www.conventionalcommits.org). main is squash-merged, so each commit
# on it is one pull request and its subject is the pull request's title.
#
#   feat: …                       minor   v0.4.2 → v0.5.0
#   fix: …   perf: …              patch   v0.4.2 → v0.4.3
#   type!: …, or a footer         major   v1.4.2 → v2.0.0
#     "BREAKING CHANGE: …"
#   anything else                 none    docs:, ci:, build:, chore:, spec:, …
#
# Before 1.0, a breaking change bumps the minor version instead: 1.0.0 is a
# decision, not something a commit message makes. The first release is v0.1.0.
#
# Only final releases — vX.Y.Z — count as the last release; a prerelease tag
# such as v0.5.0-rc.1 is ignored. scripts/next-version_test.sh covers each rule.

set -eu

last=$(git tag --merged HEAD --list 'v*' | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -n 1 || true)

if [ -n "$last" ]; then
	range="$last..HEAD"
else
	range=HEAD
fi

# 0 none, 1 patch, 2 minor, 3 major
bump=0
for commit in $(git rev-list "$range"); do
	subject=$(git log -1 --format=%s "$commit")
	body=$(git log -1 --format=%b "$commit")

	level=0
	case "$subject" in
	feat:* | feat\(*\):*) level=2 ;;
	fix:* | fix\(*\):* | perf:* | perf\(*\):*) level=1 ;;
	esac
	# "type!:" or "type(scope)!:" marks a breaking change in the subject.
	if printf '%s\n' "$subject" | grep -qE '^[a-z]+(\([^)]*\))?!: '; then
		level=3
	fi
	# So does a footer. "BREAKING-CHANGE" is a synonym in the specification.
	# The list prefix allows for a squash merge, which quotes each commit it
	# squashed as "* subject" followed by that commit's body.
	if printf '%s\n' "$body" | grep -qiE '^[[:space:]*-]*BREAKING[ -]CHANGE:'; then
		level=3
	fi

	if [ "$level" -gt "$bump" ]; then
		bump=$level
	fi
done

[ "$bump" -gt 0 ] || exit 0

if [ -z "$last" ]; then
	echo v0.1.0
	exit 0
fi

version=${last#v}
major=${version%%.*}
rest=${version#*.}
minor=${rest%%.*}
patch=${rest#*.}

if [ "$bump" -eq 3 ] && [ "$major" -eq 0 ]; then
	bump=2
fi

case "$bump" in
3) echo "v$((major + 1)).0.0" ;;
2) echo "v$major.$((minor + 1)).0" ;;
1) echo "v$major.$minor.$((patch + 1))" ;;
esac
