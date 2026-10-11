#!/usr/bin/env sh
#
# Fail unless the argument is a Conventional Commits subject kyu accepts.
#
# main is squash-merged with the pull request's title as the commit subject,
# and next-version.sh reads that subject to decide whether — and how far — to
# release. A title it cannot parse would release nothing, silently, so the
# pull request is stopped here instead.
#
#   feat: …   fix: …   perf: …               release a new version
#   type!: …  or a "BREAKING CHANGE:" footer  release a breaking version
#   docs, test, refactor, style, build, ci,  release nothing
#   chore, revert, spec, license, release

set -eu

title=${1:?usage: check-title.sh "<pull request title>"}

types='feat|fix|perf|docs|test|refactor|style|build|ci|chore|revert|spec|license|release'

if printf '%s\n' "$title" | grep -qE "^($types)(\([a-z0-9._/-]+\))?!?: [^ ]"; then
	echo "ok: $title"
	exit 0
fi

cat >&2 <<EOF
The title is not a Conventional Commits subject:

  $title

It becomes the commit subject on main, and the release version is read from
it. Write it as "type: description" or "type(scope): description", where type
is one of:

  feat      a new capability — releases a minor version
  fix       a bug fix — releases a patch version
  perf      a performance improvement — releases a patch version
  docs test refactor style build ci chore revert spec license release
            — release nothing

Add "!" after the type, as in "feat!: …", for a breaking change.
EOF
exit 1
