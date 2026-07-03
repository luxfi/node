#!/usr/bin/env bash
#
# publish_plugin_set.sh — publish the VM plugin SET to S3 from a node image.
#
# The ONE canonical way to produce lux release artifact #2 (the plugin set the
# operator's plugin-fetch init container downloads per the LuxNetwork CR
# pluginSource). It is decoupled from, and DRY with, artifact #1 (the node
# image): the plugins are ALREADY built + baked into the image by the single
# Dockerfile multi-stage build, so this step EXTRACTS them — it never compiles
# them a second time. One source of truth, two distribution surfaces.
#
# NO GitHub. Runs on any fleet host or DOKS Job that has `crane` (or docker) +
# the MinIO client `mc`. Typically invoked by the same arcd runner that just
# built + pushed the image (it has the image locally), or on demand from a
# fleet host against the published image.
#
# Usage:
#   publish_plugin_set.sh <image-ref> <s3-dest> [mc-alias]
#
#   <image-ref>   Fully-qualified node image, e.g. ghcr.io/luxfi/node:v1.30.41
#                 (digest-pinned forms are accepted and preferred for releases).
#   <s3-dest>     Bucket/prefix WITHOUT scheme, e.g.
#                 lux-plugins-testnet/v1.3.5  (matches the LuxNetwork CR
#                 pluginSource.bucket = s3://lux-plugins-testnet/v1.3.5/).
#   [mc-alias]    Configured `mc` alias for the target MinIO/S3 (default: lux).
#                 Configure once: `mc alias set lux <endpoint> <key> <secret>`.
#
# The plugin set is the 12 VM-ID files under /luxd/build/plugins/ in the image.
# A SHA256SUMS manifest (objectKey<two-spaces>sha256, one per line) is generated
# and uploaded alongside — the operator plugin-fetch init container verifies
# each plugin's sha256 against the CR (fail-closed on mismatch), so the manifest
# is also the source for the CR's pluginSource.vmPlugins[].sha256 fields.
#
# Idempotent: re-running with the same image + dest re-uploads byte-identical
# objects. A pluginset version is immutable by convention — bump <s3-dest> for a
# new release, never overwrite a published prefix that a live network points at.

set -euo pipefail

IMAGE_REF="${1:-}"
S3_DEST="${2:-}"
MC_ALIAS="${3:-lux}"

if [[ -z "${IMAGE_REF}" || -z "${S3_DEST}" ]]; then
  echo "usage: $0 <image-ref> <s3-dest> [mc-alias]" >&2
  echo "  e.g. $0 ghcr.io/luxfi/node:v1.30.41 lux-plugins-testnet/v1.3.5 lux" >&2
  exit 2
fi

# Canonical plugin set: the 12 VM-ID filenames the Dockerfile writes to
# /luxd/build/plugins/. Kept in lockstep with the Dockerfile plugin stages.
PLUGINS=(
  mgj786NP7uDwBCcq6YwThhaN8FLyybkCa4zBWTQbNgmK6k9A6   # evm    (C-Chain EVM, 0x9999)
  mDVT5EWMumBp3LCqvKwuyZQeY1VXr1jvjGNAt8nL4UFiXvqXr   # dexvm  (native D-Chain DEX)
  juFxSrbCM4wszxddKepj1GWwmrn9YgN1g4n3VUWPpRo9JjERA   # aivm
  kMhHABHM8j4bH94MCc4rsTNdo5E9En37MMyiujk4WdNxgXFsY   # bridgevm
  nZQm4Dmg1rjX18rb8maL9gamYyXPf1xCvF7ymWzxp6a1nSQTt   # graphvm
  oR6tnZHezwogyf9fRnomNXC9ojwCEBAU6jdUzpgy2PB1tD7fM   # identityvm
  pJJCSV7hHYVY6TUZwR8qUPAfuhX8JLb2C1AzNSezrYNbgau8M   # keyvm
  r5m1ujrmXxVcQetG3CQfuDLHp2RHKh6vCDaFgBRQfUcTZh7eS   # oraclevm
  ry9Sg8rZdT26iEKvJDmC2wkESs4SDKgZEhk5BgLSwg1EpcNug   # quantumvm
  sP6dLqrrBR9w3soP18fbJ3YzZecZdD7DDdfH2cFhhLq7Hy9bz   # relayvm
  tGVBwRxpmD2aFdg3iYjgRvrCe8Jcmq9UNKxyHMus2NZ8WcD8t   # mpcvm
  vv3qPfyTVXZ5ArRZA9Jh4hbYDTBe43f7sgQg4CHfNg1rnnvX9   # zkvm
)

WORK="$(mktemp -d)"
trap 'rm -rf "${WORK}"' EXIT
OUT="${WORK}/plugins"
mkdir -p "${OUT}"

echo "==> extracting plugin set from ${IMAGE_REF}"
# Build the tar member list (paths inside the image rootfs).
members=()
for id in "${PLUGINS[@]}"; do
  members+=("luxd/build/plugins/${id}")
done

# Prefer crane (no docker daemon needed). Fall back to docker create+cp.
if command -v crane >/dev/null 2>&1; then
  crane export "${IMAGE_REF}" - | tar -x -C "${WORK}" "${members[@]}"
elif command -v docker >/dev/null 2>&1; then
  cid="$(docker create "${IMAGE_REF}")"
  for id in "${PLUGINS[@]}"; do
    docker cp "${cid}:/luxd/build/plugins/${id}" "${OUT}/${id}"
  done
  docker rm -f "${cid}" >/dev/null
  # normalize layout to match crane's export path
  mkdir -p "${WORK}/luxd/build/plugins"
  mv "${OUT}"/* "${WORK}/luxd/build/plugins/" 2>/dev/null || true
  OUT="${WORK}/luxd/build/plugins"
else
  echo "FATAL: neither crane nor docker is available to extract the image" >&2
  exit 1
fi

# crane export wrote to ${WORK}/luxd/build/plugins; converge OUT to it.
[[ -d "${WORK}/luxd/build/plugins" ]] && OUT="${WORK}/luxd/build/plugins"

echo "==> generating SHA256SUMS manifest"
( cd "${OUT}"
  : > SHA256SUMS
  for id in "${PLUGINS[@]}"; do
    [[ -s "${id}" ]] || { echo "FATAL: plugin ${id} missing from image ${IMAGE_REF}" >&2; exit 1; }
    if command -v sha256sum >/dev/null 2>&1; then
      sha256sum "${id}" >> SHA256SUMS
    else
      printf '%s  %s\n' "$(shasum -a 256 "${id}" | awk '{print $1}')" "${id}" >> SHA256SUMS
    fi
  done
)
echo "----- SHA256SUMS -----"
cat "${OUT}/SHA256SUMS"
echo "----------------------"

echo "==> uploading plugin set + manifest to s3://${S3_DEST}/ (alias ${MC_ALIAS})"
# Ensure the bucket exists (no-op if present); never deletes/overwrites siblings.
bucket="${S3_DEST%%/*}"
mc mb --ignore-existing "${MC_ALIAS}/${bucket}" >/dev/null 2>&1 || true
for id in "${PLUGINS[@]}"; do
  mc cp "${OUT}/${id}" "${MC_ALIAS}/${S3_DEST}/"
done
mc cp "${OUT}/SHA256SUMS" "${MC_ALIAS}/${S3_DEST}/"

echo "==> verifying round-trip integrity (remote sha == local sha)"
fail=0
while read -r want id; do
  got="$(mc cat "${MC_ALIAS}/${S3_DEST}/${id}" | { sha256sum 2>/dev/null || shasum -a 256; } | awk '{print $1}')"
  if [[ "${got}" != "${want}" ]]; then
    echo "MISMATCH ${id}: local ${want} != remote ${got}" >&2
    fail=1
  else
    echo "ok ${id} ${got}"
  fi
done < "${OUT}/SHA256SUMS"
[[ "${fail}" -eq 0 ]] || { echo "FATAL: upload integrity check failed" >&2; exit 1; }

echo "==> published plugin set to s3://${S3_DEST}/ (${#PLUGINS[@]} plugins + SHA256SUMS)"
