# Preparing v2.0.0-alpha.1 Release

## Summary

We have successfully updated the Lux node codebase to use v2 module paths:
- `github.com/luxfi/node` → `github.com/luxfi/node/v2`
- `github.com/luxfi/evm` → `github.com/luxfi/evm/v2`
- `github.com/luxfi/cli` → `github.com/luxfi/cli/v2`

## Current Status

1. **Node Repository** (`/Users/z/work/lux/node`):
   - ✅ Module path updated to `github.com/luxfi/node/v2`
   - ✅ All imports updated to use v2 path
   - ✅ Dependencies specify v2.0.0-alpha.1 for evm and cli
   - ⏳ Needs: Commit and tag v2.0.0-alpha.1

2. **EVM Repository** (`/Users/z/work/lux/evm`):
   - ✅ Module path updated to `github.com/luxfi/evm/v2`
   - ✅ Update script created (`update_evm_to_v2.sh`)
   - ⏳ Needs: Run update script, commit, and tag v2.0.0-alpha.1

3. **CLI Repository** (`/Users/z/work/lux/cli`):
   - ✅ Module path updated to `github.com/luxfi/cli/v2`
   - ✅ Update script created (`update_cli_to_v2.sh`)
   - ⏳ Needs: Run update script, commit, and tag v2.0.0-alpha.1

## Next Steps

### 1. Tag EVM Repository
```bash
cd /Users/z/work/lux/evm
bash update_evm_to_v2.sh
git add .
git commit -m "feat: update to v2 module path for version 2.0.0-alpha.1"
git tag v2.0.0-alpha.1
git push origin dev
git push origin v2.0.0-alpha.1
```

### 2. Tag CLI Repository
```bash
cd /Users/z/work/lux/cli
bash update_cli_to_v2.sh
git add .
git commit -m "feat: update to v2 module path for version 2.0.0-alpha.1"
git tag v2.0.0-alpha.1
git push origin dev
git push origin v2.0.0-alpha.1
```

### 3. Tag Node Repository
```bash
cd /Users/z/work/lux/node
git add .
git commit -m "feat: update to v2 module path for version 2.0.0-alpha.1"
git tag v2.0.0-alpha.1
git push origin dev
git push origin v2.0.0-alpha.1
```

### 4. Verify and Build
```bash
cd /Users/z/work/lux/node
go mod tidy
go build ./...
```

## Important Notes

- All three repositories must be tagged before the modules can properly reference each other
- The v2 in the module path requires tags to start with `v2.` (e.g., v2.0.0-alpha.1)
- Replace directives should only be used for local development, not in committed code