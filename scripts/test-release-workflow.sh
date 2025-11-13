#!/usr/bin/env bash

# Test script for GitHub Actions release workflow
# Usage: ./scripts/test-release-workflow.sh [VERSION]

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${1:-1.99.99-test}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${BLUE}ℹ${NC} $*"
}

log_success() {
    echo -e "${GREEN}✓${NC} $*"
}

log_warning() {
    echo -e "${YELLOW}⚠${NC} $*"
}

log_error() {
    echo -e "${RED}✗${NC} $*"
}

# Validate version format
validate_version() {
    local ver="$1"

    # Check format: X.Y.Z or X.Y.Z-suffix or X.Y.Z+build
    if ! echo "$ver" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+([+-][a-zA-Z0-9.]+)?$'; then
        log_error "Invalid version format: $ver"
        log_info "Expected format: X.Y.Z, X.Y.Z-rc.1, or X.Y.Z+build123"
        return 1
    fi

    # Check major version < 2
    local major
    major=$(echo "$ver" | cut -d. -f1)

    if [ "$major" -ge 2 ]; then
        log_error "Version $ver is >= v2.0.0"
        log_warning "Go modules require /v2 suffix for v2+ versions"
        log_info "Only v1.x.x versions are allowed"
        return 1
    fi

    log_success "Version $ver is valid"
    return 0
}

# Check if git is clean
check_git_clean() {
    if ! git diff-index --quiet HEAD --; then
        log_warning "Working directory has uncommitted changes"
        log_info "Consider committing changes before creating release"
        read -p "Continue anyway? (y/N) " -n 1 -r
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            exit 1
        fi
    else
        log_success "Working directory is clean"
    fi
}

# Check if tag exists
check_tag_exists() {
    local tag="v$1"

    if git rev-parse "$tag" >/dev/null 2>&1; then
        log_error "Tag $tag already exists"
        log_info "Delete with: git tag -d $tag && git push --delete origin $tag"
        return 1
    fi

    log_success "Tag $tag does not exist (good)"
    return 0
}

# Check GitHub CLI
check_gh_cli() {
    if ! command -v gh &> /dev/null; then
        log_error "GitHub CLI (gh) not found"
        log_info "Install from: https://cli.github.com/"
        return 1
    fi

    if ! gh auth status &> /dev/null; then
        log_error "GitHub CLI not authenticated"
        log_info "Run: gh auth login"
        return 1
    fi

    log_success "GitHub CLI is ready"
    return 0
}

# Create test tag
create_tag() {
    local ver="$1"
    local tag="v$ver"

    log_info "Creating tag $tag..."

    git tag -a "$tag" -m "Test release workflow for $tag"

    log_success "Tag $tag created locally"
}

# Push tag
push_tag() {
    local ver="$1"
    local tag="v$ver"

    log_info "Pushing tag $tag to remote..."

    git push origin "$tag"

    log_success "Tag $tag pushed to remote"
    log_info "GitHub Actions workflow should start shortly"
}

# Monitor workflow
monitor_workflow() {
    local tag="v$1"

    log_info "Monitoring GitHub Actions workflow..."
    log_info "URL: https://github.com/luxfi/node/actions/workflows/release.yml"

    if command -v gh &> /dev/null && gh auth status &> /dev/null 2>&1; then
        log_info "Opening workflow in browser..."
        sleep 2
        gh workflow view release.yml --web
    else
        log_warning "GitHub CLI not available for monitoring"
        log_info "Check manually: https://github.com/luxfi/node/actions"
    fi
}

# Cleanup test release
cleanup() {
    local ver="$1"
    local tag="v$ver"

    log_warning "Cleaning up test release $tag..."

    # Delete remote tag
    if git ls-remote --tags origin | grep -q "refs/tags/$tag"; then
        log_info "Deleting remote tag..."
        git push --delete origin "$tag" 2>/dev/null || true
    fi

    # Delete local tag
    if git rev-parse "$tag" >/dev/null 2>&1; then
        log_info "Deleting local tag..."
        git tag -d "$tag" 2>/dev/null || true
    fi

    # Delete GitHub Release
    if command -v gh &> /dev/null && gh auth status &> /dev/null 2>&1; then
        log_info "Deleting GitHub Release..."
        gh release delete "$tag" --yes 2>/dev/null || log_warning "Release not found or already deleted"
    fi

    log_success "Cleanup complete"
}

# Main script
main() {
    log_info "Lux Node Release Workflow Test Script"
    echo ""

    # Validate version
    log_info "Validating version: $VERSION"
    if ! validate_version "$VERSION"; then
        exit 1
    fi
    echo ""

    # Check prerequisites
    log_info "Checking prerequisites..."
    check_git_clean
    check_tag_exists "$VERSION"
    check_gh_cli
    echo ""

    # Confirm test
    log_warning "This will create and push tag v$VERSION"
    log_info "The tag name should contain 'test' to avoid confusion with real releases"
    read -p "Continue? (y/N) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        log_info "Aborted"
        exit 0
    fi
    echo ""

    # Create and push tag
    create_tag "$VERSION"
    push_tag "$VERSION"
    echo ""

    # Monitor workflow
    monitor_workflow "$VERSION"
    echo ""

    # Wait for workflow to finish
    log_info "Waiting for workflow to complete..."
    log_warning "This may take 15-20 minutes"
    log_info "Press Ctrl+C to stop waiting (workflow will continue in background)"
    echo ""

    # Offer cleanup
    read -p "Do you want to clean up the test release when done? (Y/n) " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Nn]$ ]]; then
        log_info "Cleanup skipped"
        log_warning "Don't forget to manually clean up:"
        log_info "  git push --delete origin v$VERSION"
        log_info "  git tag -d v$VERSION"
        log_info "  gh release delete v$VERSION --yes"
        exit 0
    fi
    echo ""

    # Wait a bit for workflow to register
    log_info "Waiting 30 seconds for workflow to start..."
    sleep 30

    # Poll for completion (if gh CLI available)
    if command -v gh &> /dev/null && gh auth status &> /dev/null 2>&1; then
        log_info "Polling for workflow completion..."

        max_wait=1800  # 30 minutes
        elapsed=0
        interval=30

        while [ $elapsed -lt $max_wait ]; do
            # Get workflow status
            status=$(gh run list --workflow=release.yml --json status,conclusion --limit 1 | \
                     jq -r '.[0] | "\(.status):\(.conclusion)"' 2>/dev/null || echo "unknown")

            if [ "$status" = "completed:success" ]; then
                log_success "Workflow completed successfully!"
                break
            elif [ "$status" = "completed:failure" ] || [ "$status" = "completed:cancelled" ]; then
                log_error "Workflow failed: $status"
                log_info "Check logs: https://github.com/luxfi/node/actions/workflows/release.yml"
                break
            elif [ "$status" != "unknown" ]; then
                log_info "Workflow status: $status (waited ${elapsed}s)"
            fi

            sleep $interval
            elapsed=$((elapsed + interval))
        done

        if [ $elapsed -ge $max_wait ]; then
            log_warning "Timeout waiting for workflow (waited ${max_wait}s)"
        fi
    else
        log_warning "GitHub CLI not available - cannot poll for completion"
        log_info "Check manually: https://github.com/luxfi/node/actions"
        read -p "Press Enter when workflow is complete..."
    fi
    echo ""

    # Cleanup
    cleanup "$VERSION"

    log_success "Test complete!"
}

# Run main script
cd "$REPO_ROOT"
main
