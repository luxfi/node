#!/usr/bin/env bash

set -euo pipefail

# Assume the system-installed task is compatible with the taskfile version
if command -v task > /dev/null 2>&1; then
  exec task "${@}"
else
  # `task` is a *build orchestrator* that runs on the host. When a caller
  # cross-compiles the final artifact (e.g. the build-macos-release workflow
  # sets `GOOS=darwin GOARCH=arm64` on a Linux runner), those vars would be
  # inherited by `go run task` and make Go cross-compile the task launcher
  # itself for the target OS, which then can't exec on the host:
  #   fork/exec .../task: exec format error
  # Build the launcher for the HOST platform (GOOS/GOARCH unset) so it runs,
  # then exec it. The downstream `go build` inside the task still sees the
  # caller's cross-compile env via the exported GOOS/GOARCH below.
  bin_dir="$(go env GOPATH)/bin"
  mkdir -p "${bin_dir}"
  task_bin="${bin_dir}/task"
  # Cross-compile target for the actual artifact (preserved for the child).
  target_goos="${GOOS:-}"
  target_goarch="${GOARCH:-}"
  # Install the launcher for the host (clear cross-compile vars for this
  # build so the binary is host-native and execable).
  env -u GOOS -u GOARCH GOBIN="${bin_dir}" go install github.com/go-task/task/v3/cmd/task@v3.39.2
  # Re-export the cross-compile target so the build step inside the task
  # (scripts/build.sh -> `go build`) produces the requested artifact.
  if [ -n "${target_goos}" ]; then
    export GOOS="${target_goos}"
  fi
  if [ -n "${target_goarch}" ]; then
    export GOARCH="${target_goarch}"
  fi
  exec "${task_bin}" "${@}"
fi
