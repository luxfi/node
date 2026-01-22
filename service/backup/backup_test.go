// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package backup

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/luxfi/database"
	"github.com/luxfi/database/badgerdb"
	"github.com/luxfi/log"
	"github.com/luxfi/metric"
	"github.com/stretchr/testify/require"
)

// newTestDB creates a badgerdb for testing (supports backup/restore).
func newTestDB(t *testing.T) database.Database {
	t.Helper()
	tmpDir := t.TempDir()
	db, err := badgerdb.New(tmpDir, nil, "", metric.NewRegistry())
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

func TestBackupService_New(t *testing.T) {
	require := require.New(t)

	// Test missing database
	_, err := New(Config{
		MetadataDir: t.TempDir(),
	})
	require.Error(err)
	require.Contains(err.Error(), "database is required")

	// Test missing metadata directory
	db := newTestDB(t)

	_, err = New(Config{
		DB: db,
	})
	require.Error(err)
	require.Contains(err.Error(), "metadata directory is required")

	// Test successful creation
	svc, err := New(Config{
		DB:          db,
		MetadataDir: t.TempDir(),
		Log:         log.New("test", "backup"),
	})
	require.NoError(err)
	require.NotNil(svc)
}

func TestBackupService_BackupRestore(t *testing.T) {
	require := require.New(t)

	// Create source database with test data
	srcDB := newTestDB(t)

	testData := map[string]string{
		"key1": "value1",
		"key2": "value2",
		"key3": "value3",
	}
	for k, v := range testData {
		require.NoError(srcDB.Put([]byte(k), []byte(v)))
	}

	tmpDir := t.TempDir()

	// Create backup service
	svc, err := New(Config{
		DB:          srcDB,
		MetadataDir: filepath.Join(tmpDir, "meta"),
		Log:         log.New("test", "backup"),
	})
	require.NoError(err)

	// Perform backup (uncompressed)
	var buf bytes.Buffer
	version, err := svc.Backup(&buf, 0, false)
	require.NoError(err)
	require.Greater(version, uint64(0))

	backupData := buf.Bytes()
	require.NotEmpty(backupData)

	// Create destination database
	dstDB := newTestDB(t)

	dstSvc, err := New(Config{
		DB:          dstDB,
		MetadataDir: filepath.Join(tmpDir, "meta2"),
		Log:         log.New("test", "backup"),
	})
	require.NoError(err)

	// Restore
	err = dstSvc.Restore(bytes.NewReader(backupData), false)
	require.NoError(err)

	// Verify data
	for k, expected := range testData {
		actual, err := dstDB.Get([]byte(k))
		require.NoError(err)
		require.Equal(expected, string(actual))
	}
}

func TestBackupService_BackupToFile(t *testing.T) {
	require := require.New(t)

	// Create database with test data
	db := newTestDB(t)

	require.NoError(db.Put([]byte("test"), []byte("data")))

	tmpDir := t.TempDir()

	svc, err := New(Config{
		DB:          db,
		MetadataDir: filepath.Join(tmpDir, "meta"),
		Log:         log.New("test", "backup"),
	})
	require.NoError(err)

	// Test uncompressed backup
	uncompressedPath := filepath.Join(tmpDir, "backup.db")
	version, err := svc.BackupToFile(uncompressedPath, 0, false)
	require.NoError(err)
	require.Greater(version, uint64(0))

	info, err := os.Stat(uncompressedPath)
	require.NoError(err)
	require.Greater(info.Size(), int64(0))

	// Test compressed backup
	compressedPath := filepath.Join(tmpDir, "backup.zst")
	version2, err := svc.BackupToFile(compressedPath, 0, true)
	require.NoError(err)
	require.Greater(version2, uint64(0))

	info2, err := os.Stat(compressedPath)
	require.NoError(err)
	require.Greater(info2.Size(), int64(0))
}

func TestBackupService_RestoreFromFile(t *testing.T) {
	require := require.New(t)

	// Create source database with test data
	srcDB := newTestDB(t)

	require.NoError(srcDB.Put([]byte("restore-test"), []byte("restore-value")))

	tmpDir := t.TempDir()

	srcSvc, err := New(Config{
		DB:          srcDB,
		MetadataDir: filepath.Join(tmpDir, "meta-src"),
		Log:         log.New("test", "backup"),
	})
	require.NoError(err)

	// Create compressed backup
	backupPath := filepath.Join(tmpDir, "backup.zst")
	_, err = srcSvc.BackupToFile(backupPath, 0, true)
	require.NoError(err)

	// Create destination database
	dstDB := newTestDB(t)

	dstSvc, err := New(Config{
		DB:          dstDB,
		MetadataDir: filepath.Join(tmpDir, "meta-dst"),
		Log:         log.New("test", "backup"),
	})
	require.NoError(err)

	// Restore from file
	err = dstSvc.RestoreFromFile(backupPath, true)
	require.NoError(err)

	// Verify data
	val, err := dstDB.Get([]byte("restore-test"))
	require.NoError(err)
	require.Equal("restore-value", string(val))
}

func TestBackupService_Metadata(t *testing.T) {
	require := require.New(t)

	db := newTestDB(t)

	require.NoError(db.Put([]byte("meta-test"), []byte("meta-value")))

	tmpDir := t.TempDir()
	metaDir := filepath.Join(tmpDir, "meta")

	svc, err := New(Config{
		DB:          db,
		MetadataDir: metaDir,
		Log:         log.New("test", "backup"),
	})
	require.NoError(err)

	// Initially no metadata
	meta := svc.GetMetadata()
	require.Equal(uint64(0), meta.LastVersion)

	// Perform backup
	var buf bytes.Buffer
	version, err := svc.Backup(&buf, 0, false)
	require.NoError(err)

	// Check metadata updated
	meta = svc.GetMetadata()
	require.Equal(version, meta.LastVersion)
	require.False(meta.Incremental)

	// Verify metadata file exists
	metaPath := filepath.Join(metaDir, MetadataFileName)
	_, err = os.Stat(metaPath)
	require.NoError(err)

	// Create new service - should load metadata
	svc2, err := New(Config{
		DB:          db,
		MetadataDir: metaDir,
		Log:         log.New("test", "backup"),
	})
	require.NoError(err)

	meta2 := svc2.GetMetadata()
	require.Equal(version, meta2.LastVersion)
}

func TestBackupService_IncrementalBackup(t *testing.T) {
	require := require.New(t)

	db := newTestDB(t)

	// Initial data
	require.NoError(db.Put([]byte("key1"), []byte("value1")))

	tmpDir := t.TempDir()

	svc, err := New(Config{
		DB:          db,
		MetadataDir: filepath.Join(tmpDir, "meta"),
		Log:         log.New("test", "backup"),
	})
	require.NoError(err)

	// Full backup
	var fullBuf bytes.Buffer
	fullVersion, err := svc.Backup(&fullBuf, 0, false)
	require.NoError(err)

	meta := svc.GetMetadata()
	require.False(meta.Incremental)

	// Add more data
	require.NoError(db.Put([]byte("key2"), []byte("value2")))

	// Incremental backup
	var incrBuf bytes.Buffer
	incrVersion, err := svc.Backup(&incrBuf, fullVersion, false)
	require.NoError(err)
	require.GreaterOrEqual(incrVersion, fullVersion)

	meta = svc.GetMetadata()
	require.True(meta.Incremental)
}

func TestBackupService_GetLastVersion(t *testing.T) {
	require := require.New(t)

	db := newTestDB(t)

	require.NoError(db.Put([]byte("version-test"), []byte("data")))

	tmpDir := t.TempDir()

	svc, err := New(Config{
		DB:          db,
		MetadataDir: filepath.Join(tmpDir, "meta"),
		Log:         log.New("test", "backup"),
	})
	require.NoError(err)

	// Initially 0
	require.Equal(uint64(0), svc.GetLastVersion())

	// After backup, should have version
	var buf bytes.Buffer
	version, err := svc.Backup(&buf, 0, false)
	require.NoError(err)
	require.Equal(version, svc.GetLastVersion())
}
