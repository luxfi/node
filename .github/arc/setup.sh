#!/usr/bin/env bash
# ARC (Actions Runner Controller) v2 setup for lux-k8s
# Provides self-hosted GitHub Actions runners for luxfi org
#
# Prerequisites:
#   1. Create a GitHub App for ARC authentication:
#      - Go to https://github.com/organizations/luxfi/settings/apps/new
#      - App name: lux-arc-runner
#      - Homepage: https://lux.network
#      - Permissions:
#        - Organization: Self-hosted runners (Read & Write)
#        - Organization: Administration (Read-only)
#        - Repository: Actions (Read-only), Metadata (Read-only)
#      - Generate a private key (.pem file)
#      - Install the app on the luxfi organization (all repos)
#      - Note the App ID and Installation ID
#
#   2. Set env vars before running this script:
#      export GITHUB_APP_ID=<app-id>
#      export GITHUB_APP_INSTALLATION_ID=<installation-id>
#      export GITHUB_APP_PRIVATE_KEY_FILE=<path-to-pem>
#
# Usage:
#   ./setup.sh install    # Install ARC controller + runner scale set
#   ./setup.sh uninstall  # Remove everything

set -euo pipefail

KUBE_CONTEXT="do-sfo3-lux-k8s"
CONTROLLER_NS="arc-system"
RUNNER_NS="arc-runners"
CONTROLLER_CHART="oci://ghcr.io/actions/actions-runner-controller-charts/gha-runner-scale-set-controller"
RUNNER_CHART="oci://ghcr.io/actions/actions-runner-controller-charts/gha-runner-scale-set"

kc() {
  kubectl --context "$KUBE_CONTEXT" "$@"
}

install_controller() {
  echo "==> Creating namespaces..."
  kc create namespace "$CONTROLLER_NS" --dry-run=client -o yaml | kc apply -f -
  kc create namespace "$RUNNER_NS" --dry-run=client -o yaml | kc apply -f -

  echo "==> Installing ARC controller..."
  helm --kube-context "$KUBE_CONTEXT" upgrade --install arc \
    "$CONTROLLER_CHART" \
    --namespace "$CONTROLLER_NS" \
    --set replicaCount=1 \
    --wait

  echo "==> ARC controller installed."
}

install_runners() {
  if [ -z "${GITHUB_APP_ID:-}" ] || [ -z "${GITHUB_APP_INSTALLATION_ID:-}" ] || [ -z "${GITHUB_APP_PRIVATE_KEY_FILE:-}" ]; then
    echo "ERROR: Set GITHUB_APP_ID, GITHUB_APP_INSTALLATION_ID, GITHUB_APP_PRIVATE_KEY_FILE"
    exit 1
  fi

  # Create the secret with GitHub App credentials
  echo "==> Creating GitHub App secret..."
  kc create secret generic arc-github-app \
    --namespace "$RUNNER_NS" \
    --from-literal=github_app_id="$GITHUB_APP_ID" \
    --from-literal=github_app_installation_id="$GITHUB_APP_INSTALLATION_ID" \
    --from-file=github_app_private_key="$GITHUB_APP_PRIVATE_KEY_FILE" \
    --dry-run=client -o yaml | kc apply -f -

  echo "==> Installing runner scale set (linux-amd64)..."
  helm --kube-context "$KUBE_CONTEXT" upgrade --install arc-runner-set \
    "$RUNNER_CHART" \
    --namespace "$RUNNER_NS" \
    --values "$(dirname "$0")/values-runner.yaml" \
    --wait

  echo "==> Runner scale set installed."
  echo ""
  echo "Runners will appear in GitHub as: self-hosted, linux, amd64, lux-build"
  echo "Use in workflows with: runs-on: [self-hosted, linux, amd64, lux-build]"
}

uninstall() {
  echo "==> Uninstalling runner scale set..."
  helm --kube-context "$KUBE_CONTEXT" uninstall arc-runner-set --namespace "$RUNNER_NS" 2>/dev/null || true

  echo "==> Uninstalling ARC controller..."
  helm --kube-context "$KUBE_CONTEXT" uninstall arc --namespace "$CONTROLLER_NS" 2>/dev/null || true

  echo "==> Cleaning up namespaces..."
  kc delete namespace "$RUNNER_NS" --ignore-not-found
  kc delete namespace "$CONTROLLER_NS" --ignore-not-found

  echo "==> Uninstalled."
}

case "${1:-help}" in
  install)
    install_controller
    install_runners
    ;;
  controller)
    install_controller
    ;;
  runners)
    install_runners
    ;;
  uninstall)
    uninstall
    ;;
  *)
    echo "Usage: $0 {install|controller|runners|uninstall}"
    exit 1
    ;;
esac
