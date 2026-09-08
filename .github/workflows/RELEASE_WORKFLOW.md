# Release Workflow Documentation

## Overview

The `release.yml` workflow automates building and publishing Lux Node binaries for all platforms when semantic version tags are pushed.

## Workflow Architecture

### 1. Trigger Mechanism

**Tag Pattern**: `v[0-1].*.*`
- ✅ Accepts: `v1.0.0`, `v1.20.1`, `v1.999.999`, `v1.20.1-rc.1`
- ❌ Rejects: `v2.0.0`, `v2.1.0`, `v3.0.0` (requires `/v2` import path per Go modules)

**Rationale**: Go modules require major version 2+ to use `/v2`, `/v3` etc. in import paths. Since Lux uses `github.com/luxfi/node` (no version suffix), we enforce v1.x.x only.

### 2. Job Flow

```
validate-version (validates tag < v2.0.0)
    ├─> build-ubuntu-amd64 ───┐
    ├─> build-ubuntu-arm64 ───┤
    ├─> build-macos ──────────┼──> create-release (combines all artifacts)
    └─> build-windows ────────┘
```

### 3. Platform Builds

Uses **reusable workflows** (existing build-*-release.yml files):

| Platform | Workflow | Artifact Name | Binary Format |
|----------|----------|---------------|---------------|
| Linux AMD64 (Ubuntu 22.04) | `build-ubuntu-amd64-release.yml` | `jammy` | `.deb` package |
| Linux AMD64 (Ubuntu 20.04) | `build-ubuntu-amd64-release.yml` | `focal` | `.deb` package |
| Linux ARM64 (Ubuntu 22.04) | `build-ubuntu-arm64-release.yml` | `jammy` | `.deb` package |
| Linux ARM64 (Ubuntu 20.04) | `build-ubuntu-arm64-release.yml` | `focal` | `.deb` package |
| macOS (Universal) | `build-macos-release.yml` | `build` | `.zip` archive |
| Windows AMD64 | `build-win-release.yml` | Various | `.exe` or `.zip` |

### 4. Release Creation

**GitHub Release includes**:
- All platform binaries
- `SHA256SUMS` checksum file
- Auto-generated changelog (git log since previous tag)
- Pre-release flag (if version contains `-` or `+`)
- "Latest" badge (for stable releases only)

## Usage

### Creating a Release

1. **Tag the commit**:
   ```bash
   git tag -a v1.20.1 -m "Release v1.20.1"
   git push origin v1.20.1
   ```

2. **Monitor workflow**:
   - Visit: https://github.com/luxfi/node/actions/workflows/release.yml
   - Watch all build jobs complete (typically 15-20 minutes)

3. **Verify release**:
   - Visit: https://github.com/luxfi/node/releases
   - Check all platform binaries are attached
   - Verify SHA256SUMS file

### Pre-release Creation

For release candidates or beta versions:

```bash
git tag -a v1.20.1-rc.1 -m "Release Candidate 1 for v1.20.1"
git push origin v1.20.1-rc.1
```

**Behavior**:
- Creates GitHub Release with "Pre-release" badge
- Does NOT mark as "Latest"
- Changelog includes "(Pre-release)" note

### Deleting a Failed Release

If a release fails and needs to be retried:

```bash
# Delete remote tag
git push --delete origin v1.20.1

# Delete local tag
git tag -d v1.20.1

# Delete GitHub Release (via web UI or gh CLI)
gh release delete v1.20.1 --yes

# Fix issues, then re-tag and push
git tag -a v1.20.1 -m "Release v1.20.1"
git push origin v1.20.1
```

## Testing the Workflow

### Dry Run (Test Tag)

Create a test tag to verify workflow without publishing:

```bash
# Create test tag locally
git tag -a v1.99.99-test -m "Test release workflow"

# Push to remote (triggers workflow)
git push origin v1.99.99-test

# After testing, clean up
git push --delete origin v1.99.99-test
git tag -d v1.99.99-test
gh release delete v1.99.99-test --yes
```

### Local Validation

Test semver validation logic locally:

```bash
# Test valid versions
for ver in 1.0.0 1.20.1 1.999.999 "1.20.1-rc.1"; do
    MAJOR=$(echo "$ver" | cut -d. -f1)
    if [ "$MAJOR" -ge 2 ]; then
        echo "✗ v${ver}: REJECT"
    else
        echo "✓ v${ver}: ACCEPT"
    fi
done

# Test invalid versions
for ver in 2.0.0 2.1.0 3.0.0; do
    MAJOR=$(echo "$ver" | cut -d. -f1)
    if [ "$MAJOR" -ge 2 ]; then
        echo "✓ v${ver}: REJECT (correct)"
    else
        echo "✗ v${ver}: ACCEPT (should reject)"
    fi
done
```

## Troubleshooting

### Issue: "Version >= v2.0.0" Error

**Cause**: Attempted to tag v2.x.x or higher

**Solution**: Use v1.x.x versions only. For v2+, update import paths to `github.com/luxfi/node/v2` throughout codebase first.

### Issue: Build Workflow Fails

**Symptoms**: `create-release` job never runs

**Diagnosis**:
1. Check individual build job logs
2. Common issues:
   - AWS credentials expired (check secrets)
   - Build script failures (check `./scripts/run_task.sh build`)
   - Dependency resolution issues

**Solution**:
1. Fix build issues in individual workflow
2. Delete failed release tag
3. Re-tag and push

### Issue: Missing Artifacts

**Symptoms**: Some platform binaries not attached to release

**Diagnosis**:
1. Check `Download all artifacts` step in `create-release` job
2. Verify artifact names match expected patterns

**Solution**:
1. Update `Organize release files` step to match actual artifact structure
2. Check individual build workflows upload artifacts correctly

### Issue: Changelog Empty

**Symptoms**: Release notes say "Initial Release" but previous tags exist

**Cause**: Shallow git checkout (missing history)

**Solution**: Workflow uses `fetch-depth: 0` to fetch full history. If issue persists:
1. Check git repository configuration
2. Verify previous tags are pushed to remote

## Security Considerations

### Permissions

Workflow requires minimal permissions:
- `contents: write` - Create releases and upload assets
- `id-token: write` - AWS OIDC authentication (for build workflows)

### Secrets Required

All secrets inherited from repository settings:
- `AWS_DEPLOY_SA_ROLE_ARN` - AWS role for S3 uploads (build workflows)
- `BUCKET` - S3 bucket name (build workflows)
- `GITHUB_TOKEN` - Automatically provided by GitHub Actions

### Checksum Verification

Users can verify downloads:

```bash
# Download release binary and SHA256SUMS
curl -LO https://github.com/luxfi/node/releases/download/v1.20.1/luxd-macos-v1.20.1.zip
curl -LO https://github.com/luxfi/node/releases/download/v1.20.1/SHA256SUMS

# Verify checksum
grep luxd-macos-v1.20.1.zip SHA256SUMS | sha256sum -c
```

## Workflow Outputs

### GitHub Release

**URL Format**: `https://github.com/luxfi/node/releases/tag/v{VERSION}`

**Contains**:
- Release title: "Lux Node v{VERSION}"
- Changelog: Auto-generated from git commits
- Assets:
  - `luxd-{version}-amd64.deb` (Ubuntu 22.04)
  - `luxd-{version}-amd64.deb` (Ubuntu 20.04)
  - `luxd-{version}-arm64.deb` (Ubuntu 22.04)
  - `luxd-{version}-arm64.deb` (Ubuntu 20.04)
  - `luxd-macos-{version}.zip`
  - `node-win-{version}.zip` or `.exe`
  - `SHA256SUMS`

### Job Summary

GitHub Actions summary page shows:
- Release version
- Platform artifact table (file names, sizes)
- SHA256 checksums
- Link to release page

## Maintenance

### Adding New Platforms

To add a new platform (e.g., FreeBSD):

1. Create new build workflow: `.github/workflows/build-freebsd-release.yml`
2. Add job to `release.yml`:
   ```yaml
   build-freebsd:
     needs: validate-version
     uses: ./.github/workflows/build-freebsd-release.yml
     secrets: inherit
   ```
3. Update `create-release` job dependencies:
   ```yaml
   needs:
     - validate-version
     - build-ubuntu-amd64
     - build-ubuntu-arm64
     - build-macos
     - build-windows
     - build-freebsd  # Add here
   ```
4. Update artifact collection logic in `Organize release files` step

### Updating Changelog Format

Edit the `Generate changelog` step:

```yaml
- name: Generate changelog
  run: |
    # Custom changelog format
    git log --pretty=format:"- **%s** by @%an (%h)" ${PREV_TAG}..HEAD > CHANGELOG.md
```

### Customizing Release Title

Edit the `Create GitHub Release` step:

```yaml
FLAGS="--title \"Lux Network Node ${TAG} - Codename XYZ\""
```

## Related Documentation

- [GitHub Actions Documentation](https://docs.github.com/en/actions)
- [Semantic Versioning](https://semver.org/)
- [Go Modules Version Numbering](https://go.dev/doc/modules/version-numbers)
- [GitHub CLI Release Documentation](https://cli.github.com/manual/gh_release_create)

## Support

For issues with the release workflow:
1. Check GitHub Actions logs
2. Review this documentation
3. Open issue with `ci` label
4. Contact DevOps team

---

**Last Updated**: 2025-11-12
**Maintainer**: Lux DevOps Team
