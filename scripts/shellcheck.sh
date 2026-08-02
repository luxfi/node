#!/usr/bin/env bash

set -euo pipefail

# This script can also be used to correct the problems detected by shellcheck by invoking as follows:
#
# ./scripts/tests.shellcheck.sh -f diff | git apply
#

if ! [[ "$0" =~ scripts/shellcheck.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

# The script provisions its own shellcheck, the way scripts/actionlint.sh pins
# actionlint with `go run …@v1.7.1`. shellcheck is Haskell, so there is no `go
# run` equivalent — hence the release download.
#
# Without this the lint job died on
#
#   xargs: shellcheck: No such file or directory
#
# which reads as a shell-script problem and is really the linter being absent
# from the runner image: golangci-lint reported "0 issues. ALL SUCCESS!"
# immediately before it. Pinning here rather than in the workflow keeps a laptop
# and CI running the same version.
SHELLCHECK_VERSION="v0.11.0"

if command -v shellcheck > /dev/null 2>&1; then
  SHELLCHECK=shellcheck
else
  case "$(uname -s)" in
    Linux)  SC_OS=linux  ;;
    Darwin) SC_OS=darwin ;;
    *) echo "shellcheck is not installed and $(uname -s) has no pinned release; install it manually" >&2; exit 255 ;;
  esac
  case "$(uname -m)" in
    x86_64|amd64)  SC_ARCH=x86_64  ;;
    aarch64|arm64) SC_ARCH=aarch64 ;;
    *) echo "shellcheck is not installed and $(uname -m) has no pinned release; install it manually" >&2; exit 255 ;;
  esac

  SC_DIR="${TMPDIR:-/tmp}/shellcheck-${SHELLCHECK_VERSION}-${SC_OS}-${SC_ARCH}"
  SHELLCHECK="${SC_DIR}/shellcheck-${SHELLCHECK_VERSION}/shellcheck"

  if [[ ! -x "${SHELLCHECK}" ]]; then
    echo "shellcheck not found; fetching ${SHELLCHECK_VERSION} for ${SC_OS}/${SC_ARCH}"
    mkdir -p "${SC_DIR}"
    curl -sSfL \
      "https://github.com/koalaman/shellcheck/releases/download/${SHELLCHECK_VERSION}/shellcheck-${SHELLCHECK_VERSION}.${SC_OS}.${SC_ARCH}.tar.xz" \
      | tar -xJ -C "${SC_DIR}"
  fi
fi

# `find *` is the simplest way to ensure find does not include a
# leading `.` in filenames it emits. A leading `.` will prevent the
# use of `git apply` to fix reported shellcheck issues. This is
# compatible with both macos and linux (unlike the use of -printf).
#
# shellcheck disable=SC2035
# ${@+…} because bash 3.2 — still the default /bin/bash on macOS — treats a
# bare "${@}" with zero arguments as unset under `set -u` and aborts with
#   scripts/shellcheck.sh: line 21: @: unbound variable
# bash 4.4+ (what the runner has) does not, so this only ever bit locally.
find * -name "*.sh" -type f -print0 | xargs -0 "${SHELLCHECK}" ${@+"${@}"}
