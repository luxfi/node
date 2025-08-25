# External Dependency Issues

As of 2025-08-25, the following external dependencies have compatibility issues:

## 1. github.com/luxfi/geth v1.16.34
**Issue**: tablewriter API mismatch in `core/rawdb/database.go`
```
table.SetHeader undefined (type *tablewriter.Table has no field or method SetHeader)
table.SetFooter undefined (type *tablewriter.Table has no field or method SetFooter)  
table.AppendBulk undefined (type *tablewriter.Table has no field or method AppendBulk)
```
**Action**: Need to update geth to use the correct tablewriter API or pin to compatible version

## 2. github.com/luxfi/qzmq v0.1.1
**Issue**: Missing zmq.NewStream function in `backend.go:69`
```
undefined: zmq.NewStream
```
**Action**: The qzmq package needs to be updated to use the correct ZMQ API or implement NewStream

## 3. github.com/luxfi/consensus v1.3.2
**Issue**: Engine interface missing HealthCheck method
```
"github.com/luxfi/consensus/engine/dag".Engine does not implement Engine (missing method HealthCheck)
```
**Action**: The consensus package DAG engine needs to implement the HealthCheck method

## Current Build Status
- **Core node packages**: ✅ 100% building
- **With external deps**: ⚠️ 16 packages failing due to above issues
- **Internal packages**: ✅ All internal packages compile successfully

## Recommendations
1. Report issues to respective repositories
2. Consider forking and fixing if urgent
3. Pin to last known working versions if available