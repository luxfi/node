# GitHub Actions Release Workflow - Technical Explanation

## Overview

This document explains the technical implementation of the automated release workflow for Lux Node.

## Architecture

### Workflow Design Philosophy

The release workflow follows these principles:

1. **Single Responsibility**: Each job does one thing well
2. **Reusable Components**: Leverages existing build workflows as reusable components
3. **Fail Fast**: Version validation happens first before any builds
4. **Parallel Execution**: All platform builds run concurrently
5. **Atomic Release**: All artifacts collected before release creation

### Job Dependency Graph

```
┌──────────────────┐
│ validate-version │ (30s)
└────────┬─────────┘
         │
    ┌────┴────┬───────────┬──────────┐
    │         │           │          │
┌───▼────┐ ┌─▼─────┐ ┌───▼────┐ ┌──▼─────┐
│ubuntu  │ │ubuntu │ │ macos  │ │windows │ (10-15 min each)
│amd64   │ │arm64  │ │        │ │        │
└───┬────┘ └─┬─────┘ └───┬────┘ └──┬─────┘
    └────────┴───────────┴──────────┘
                  │
           ┌──────▼────────┐
           │create-release │ (2-3 min)
           └───────────────┘
```

**Total Time**: ~15-20 minutes (parallel builds are the bottleneck)

## Job Details

### 1. validate-version

**Purpose**: Ensure semantic version is valid and < v2.0.0

**Outputs**:
- `version`: Version number without 'v' prefix (e.g., "1.20.1")
- `is_prerelease`: Boolean indicating if version is pre-release

**Logic**:
```bash
# Extract version from tag
TAG="${GITHUB_REF#refs/tags/}"  # refs/tags/v1.20.1 → v1.20.1
VERSION="${TAG#v}"               # v1.20.1 → 1.20.1

# Validate major version
MAJOR=$(echo "$VERSION" | cut -d. -f1)  # 1.20.1 → 1

if [ "$MAJOR" -ge 2 ]; then
    exit 1  # Fail workflow
fi

# Detect pre-release (contains - or +)
if echo "$VERSION" | grep -qE '[-+]'; then
    is_prerelease=true
fi
```

**Why < v2.0.0?**

Go modules semantics require v2+ to use versioned import paths:

```go
// v1.x.x (current)
import "github.com/luxfi/node/vms"

// v2.x.x would require:
import "github.com/luxfi/node/v2/vms"
```

Enforcing v1.x.x prevents accidental breaking changes to import paths.

### 2. build-* Jobs

**Pattern**: Uses `uses: ./.github/workflows/build-*-release.yml`

**Why Reusable Workflows?**
- DRY principle: Don't duplicate build logic
- Maintainability: Update build logic in one place
- Consistency: Same build process for manual and automated releases

**Secrets Inheritance**:
```yaml
secrets: inherit
```

Passes all repository secrets to reusable workflows (AWS credentials, S3 buckets, etc.)

**Input Passing**:
```yaml
with:
  tag: ${{ needs.validate-version.outputs.version }}
```

Some workflows accept tag as input for artifact naming.

### 3. create-release

**Purpose**: Collect all artifacts and create GitHub Release

**Steps**:

#### a. Download Artifacts

```yaml
uses: actions/download-artifact@v4
with:
  path: ./artifacts
```

**Result**: All build artifacts downloaded to `./artifacts/`

**Directory Structure**:
```
./artifacts/
├── jammy/
│   └── luxd-v1.20.1-amd64.deb
├── focal/
│   └── luxd-v1.20.1-amd64.deb
└── build/
    └── luxd-macos-v1.20.1.zip
```

#### b. Organize Files

Copies all artifacts to `./release/` directory with flat structure.

**Why Flatten?**
- Simpler asset URLs
- Easier for users to find files
- Consistent naming across platforms

#### c. Generate Checksums

```bash
cd ./release
sha256sum * > SHA256SUMS
```

**Format**:
```
a1b2c3d4... luxd-v1.20.1-amd64.deb
e5f6g7h8... luxd-macos-v1.20.1.zip
```

**User Verification**:
```bash
# Download file and checksums
curl -LO <file-url>
curl -LO <checksums-url>

# Verify
grep <filename> SHA256SUMS | sha256sum -c
```

#### d. Generate Changelog

Uses git log between previous tag and current:

```bash
PREV_TAG=$(git describe --tags --abbrev=0 HEAD^)
git log --pretty=format:"- %s (%h)" ${PREV_TAG}..HEAD
```

**Output Format**:
```markdown
## What's Changed

- Add new feature X (abc123)
- Fix bug in Y component (def456)
- Update dependencies (ghi789)

**Full Changelog**: https://github.com/luxfi/node/compare/v1.20.0...v1.20.1
```

#### e. Create Release

Uses `gh` CLI for release creation:

```bash
gh release create "v1.20.1" \
  ./release/* \
  --title "Lux Node v1.20.1" \
  --notes-file CHANGELOG.md \
  --latest  # or --prerelease for pre-releases
```

**Why gh CLI?**
- Official GitHub tool
- Handles authentication automatically
- Simpler than REST API
- Uploads all files in one command

## Workflow Triggers

### Tag Pattern Matching

```yaml
on:
  push:
    tags:
      - 'v[0-1].*.*'
```

**Pattern Breakdown**:
- `v` - Literal 'v' character
- `[0-1]` - Major version 0 or 1
- `.*` - Any minor version
- `.*` - Any patch version

**Matches**:
- ✅ `v1.0.0` (first release)
- ✅ `v1.20.1` (production release)
- ✅ `v1.99.999` (high version numbers)
- ✅ `v1.20.1-rc.1` (release candidate)
- ✅ `v1.20.1+build.123` (build metadata)

**Rejects**:
- ❌ `v2.0.0` (major version 2)
- ❌ `v3.1.0` (major version 3)
- ❌ `1.20.1` (missing 'v' prefix)
- ❌ `version-1.20.1` (wrong format)

## Pre-release Detection

**Logic**:
```bash
if echo "$VERSION" | grep -qE '[-+]'; then
    is_prerelease=true
fi
```

**Semantic Versioning**:
- `-` indicates pre-release: `1.20.1-rc.1`, `1.20.1-beta.2`
- `+` indicates build metadata: `1.20.1+build.123`

**GitHub Behavior**:
- Pre-releases: Shown with "Pre-release" badge, not marked as "Latest"
- Stable releases: Shown with "Latest release" badge

## Artifact Naming Conventions

Each platform has its own naming scheme:

| Platform | Pattern | Example |
|----------|---------|---------|
| Ubuntu AMD64 | `luxd-{version}-amd64.deb` | `luxd-v1.20.1-amd64.deb` |
| Ubuntu ARM64 | `luxd-{version}-arm64.deb` | `luxd-v1.20.1-arm64.deb` |
| macOS | `luxd-macos-{version}.zip` | `luxd-macos-v1.20.1.zip` |
| Windows | `node-win-{version}.zip` | `node-win-v1.20.1.zip` |

**Why Different Patterns?**
- Historical convention from existing build scripts
- Platform-specific package managers expect different formats
- Windows uses `node` instead of `luxd` (legacy naming)

## Error Handling

### Build Failure

**Scenario**: One platform build fails

**Behavior**:
- `create-release` job never runs (depends on all builds)
- Workflow marked as failed
- No partial release created

**Recovery**:
1. Fix build issue
2. Delete failed tag: `git tag -d v1.20.1 && git push --delete origin v1.20.1`
3. Re-tag and push

### Partial Artifact Collection

**Scenario**: Some artifacts missing during collection

**Behavior**:
- `Organize release files` step silently skips missing files (`|| true`)
- Release created with available artifacts
- Missing platforms will have no binary attached

**Detection**:
- Check `Release summary` in job output
- Verify all expected platforms in asset list

**Recovery**:
1. Identify which build workflow failed
2. Fix workflow
3. Manually trigger failed workflow with same tag
4. Download new artifacts
5. Upload to existing release: `gh release upload v1.20.1 <file>`

## Performance Optimization

### Parallel Builds

All platform builds run simultaneously:

```yaml
build-ubuntu-amd64:
  needs: validate-version
  # ...

build-ubuntu-arm64:
  needs: validate-version
  # ...

# All depend only on validate-version, not each other
```

**Time Savings**:
- Sequential: ~60 minutes (4 platforms × 15 min each)
- Parallel: ~15 minutes (longest build time)
- **Improvement**: 75% faster

### Shallow Checkout

Most jobs use default shallow checkout (depth=1):
```yaml
- uses: actions/checkout@v4
```

**Exception**: `create-release` job needs full history for changelog:
```yaml
- uses: actions/checkout@v4
  with:
    fetch-depth: 0
```

## Security Considerations

### Minimal Permissions

```yaml
permissions:
  contents: write    # Create releases, upload assets
  id-token: write    # OIDC for AWS authentication
```

**Not Granted**:
- `actions: write` - Cannot modify workflows
- `packages: write` - Cannot publish packages
- `issues: write` - Cannot create issues

### Secret Handling

All secrets passed via `secrets: inherit`:

```yaml
build-ubuntu-amd64:
  secrets: inherit  # Passes AWS_DEPLOY_SA_ROLE_ARN, BUCKET
```

**Best Practice**: Never log secrets, never use in conditionals

### Checksum Verification

Forces users to verify downloads:

```bash
# Generate checksums
sha256sum * > SHA256SUMS

# Users verify with:
sha256sum -c SHA256SUMS
```

## Testing Strategy

### Manual Test Tag

Create test release without polluting real releases:

```bash
# Use version with "test" keyword
git tag -a v1.99.99-test -m "Test release workflow"
git push origin v1.99.99-test

# Monitor: https://github.com/luxfi/node/actions

# Cleanup after testing
git push --delete origin v1.99.99-test
git tag -d v1.99.99-test
gh release delete v1.99.99-test --yes
```

### Automated Test Script

Use included test script:

```bash
./scripts/test-release-workflow.sh 1.99.99-test
```

**Script Features**:
- Version validation
- Git cleanliness check
- Tag conflict detection
- Workflow monitoring
- Automatic cleanup

## Debugging

### Enable Debug Logging

Add to workflow:

```yaml
env:
  ACTIONS_STEP_DEBUG: true
  ACTIONS_RUNNER_DEBUG: true
```

### Check Job Logs

1. Visit workflow run: https://github.com/luxfi/node/actions/workflows/release.yml
2. Click specific run
3. Expand failed job
4. Read error messages

### Common Issues

**Issue**: "Resource not accessible by integration"
- **Cause**: Missing `contents: write` permission
- **Fix**: Add permission to workflow

**Issue**: "Unable to download artifact"
- **Cause**: Artifact name mismatch
- **Fix**: Check `upload-artifact` name in build workflow

**Issue**: "gh: command not found"
- **Cause**: GitHub CLI not installed in runner
- **Fix**: Add `- uses: cli/gh-action@v2` or use gh CLI pre-installed

## Future Enhancements

### Potential Improvements

1. **Docker Image Publishing**: Add Docker build job
2. **Homebrew Formula Update**: Auto-update brew formula
3. **Release Notes Template**: Use `.github/release-template.md`
4. **Asset Signing**: GPG sign all binaries
5. **Download Stats**: Track download metrics
6. **Auto-changelog**: Generate from commit conventions
7. **Slack Notification**: Notify team on release
8. **Rollback Support**: Tag previous version as latest if needed

### Metrics to Track

- Build time per platform
- Artifact size trends
- Download counts per platform
- Release frequency
- Time to production

## References

- [GitHub Actions Workflow Syntax](https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions)
- [Reusable Workflows](https://docs.github.com/en/actions/using-workflows/reusing-workflows)
- [GitHub Releases](https://docs.github.com/en/repositories/releasing-projects-on-github)
- [Semantic Versioning](https://semver.org/)
- [Go Module Version Numbering](https://go.dev/doc/modules/version-numbers)

---

**Last Updated**: 2025-11-12
**Maintainer**: Lux DevOps Team
