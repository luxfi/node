// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package backup provides database backup and restore functionality.
// It supports incremental backups using ZapDB's native backup mechanism
// with optional zstd compression.
package backup

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/luxfi/database"
	"github.com/luxfi/log"
)

const (
	// DefaultChunkSize is the default size of backup chunks (1MB).
	DefaultChunkSize = 1 << 20

	// MetadataFileName is the name of the backup metadata file.
	MetadataFileName = "backup_metadata.json"
)

var (
	// ErrDatabaseClosed is returned when the database is closed.
	ErrDatabaseClosed = errors.New("database is closed")

	// ErrBackupInProgress is returned when a backup is already in progress.
	ErrBackupInProgress = errors.New("backup already in progress")

	// ErrRestoreInProgress is returned when a restore is already in progress.
	ErrRestoreInProgress = errors.New("restore already in progress")
)

// Metadata stores information about the last backup.
type Metadata struct {
	LastVersion    uint64    `json:"lastVersion"`
	LastBackupTime time.Time `json:"lastBackupTime"`
	LastBackupSize uint64    `json:"lastBackupSize"`
	Incremental    bool      `json:"incremental"`
}

// Config configures the backup service.
type Config struct {
	// DB is the database to backup/restore.
	DB database.Database
	// MetadataDir is the directory to store backup metadata.
	MetadataDir string
	// ChunkSize is the size of backup chunks in bytes.
	ChunkSize int
	// Log is the logger.
	Log log.Logger
}

// Service provides backup and restore operations.
type Service struct {
	db          database.Database
	metadataDir string
	chunkSize   int
	log         log.Logger

	mu              sync.Mutex
	backupActive    bool
	restoreActive   bool
	currentMetadata *Metadata
}

// New creates a new backup service.
func New(cfg Config) (*Service, error) {
	if cfg.DB == nil {
		return nil, errors.New("database is required")
	}
	if cfg.MetadataDir == "" {
		return nil, errors.New("metadata directory is required")
	}
	if cfg.ChunkSize <= 0 {
		cfg.ChunkSize = DefaultChunkSize
	}

	s := &Service{
		db:          cfg.DB,
		metadataDir: cfg.MetadataDir,
		chunkSize:   cfg.ChunkSize,
		log:         cfg.Log,
	}

	// Load existing metadata
	if err := s.loadMetadata(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load backup metadata: %w", err)
	}

	return s, nil
}

// Backup performs a database backup, writing to the provided writer.
// since: 0 for full backup, or the version from the last backup for incremental.
// compress: whether to compress the output with zstd.
// Returns the new version number for future incremental backups.
func (s *Service) Backup(w io.Writer, since uint64, compress bool) (uint64, error) {
	s.mu.Lock()
	if s.backupActive {
		s.mu.Unlock()
		return 0, ErrBackupInProgress
	}
	s.backupActive = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.backupActive = false
		s.mu.Unlock()
	}()

	var writer io.Writer = w
	var encoder *zstd.Encoder
	var err error

	if compress {
		encoder, err = zstd.NewWriter(w, zstd.WithEncoderLevel(zstd.SpeedDefault))
		if err != nil {
			return 0, fmt.Errorf("failed to create zstd encoder: %w", err)
		}
		defer encoder.Close()
		writer = encoder
	}

	// Perform the backup
	version, err := s.db.Backup(writer, since)
	if err != nil {
		return 0, fmt.Errorf("backup failed: %w", err)
	}

	// Ensure compressed data is flushed
	if encoder != nil {
		if err := encoder.Close(); err != nil {
			return 0, fmt.Errorf("failed to close zstd encoder: %w", err)
		}
	}

	// Update metadata
	s.mu.Lock()
	s.currentMetadata = &Metadata{
		LastVersion:    version,
		LastBackupTime: time.Now(),
		Incremental:    since > 0,
	}
	s.mu.Unlock()

	if err := s.saveMetadata(); err != nil {
		s.log.Warn("failed to save backup metadata", "error", err)
	}

	s.log.Info("backup completed",
		"version", version,
		"since", since,
		"incremental", since > 0,
	)

	return version, nil
}

// BackupToFile performs a backup to a file path.
func (s *Service) BackupToFile(path string, since uint64, compress bool) (uint64, error) {
	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return 0, fmt.Errorf("failed to create backup directory: %w", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return 0, fmt.Errorf("failed to create backup file: %w", err)
	}
	defer f.Close()

	version, err := s.Backup(f, since, compress)
	if err != nil {
		os.Remove(path) // Clean up on failure
		return 0, err
	}

	// Get file size for metadata
	info, err := f.Stat()
	if err == nil {
		s.mu.Lock()
		if s.currentMetadata != nil {
			s.currentMetadata.LastBackupSize = uint64(info.Size())
		}
		s.mu.Unlock()
		s.saveMetadata()
	}

	return version, nil
}

// Restore restores the database from the provided reader.
// The reader should contain backup data (optionally zstd compressed).
func (s *Service) Restore(r io.Reader, compressed bool) error {
	s.mu.Lock()
	if s.restoreActive {
		s.mu.Unlock()
		return ErrRestoreInProgress
	}
	s.restoreActive = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.restoreActive = false
		s.mu.Unlock()
	}()

	var reader io.Reader = r

	if compressed {
		decoder, err := zstd.NewReader(r)
		if err != nil {
			return fmt.Errorf("failed to create zstd decoder: %w", err)
		}
		defer decoder.Close()
		reader = decoder
	}

	if err := s.db.Load(reader); err != nil {
		return fmt.Errorf("restore failed: %w", err)
	}

	s.log.Info("restore completed")
	return nil
}

// RestoreFromFile restores the database from a file path.
func (s *Service) RestoreFromFile(path string, compressed bool) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open backup file: %w", err)
	}
	defer f.Close()

	return s.Restore(f, compressed)
}

// GetMetadata returns the current backup metadata.
func (s *Service) GetMetadata() *Metadata {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.currentMetadata == nil {
		return &Metadata{}
	}

	// Return a copy
	return &Metadata{
		LastVersion:    s.currentMetadata.LastVersion,
		LastBackupTime: s.currentMetadata.LastBackupTime,
		LastBackupSize: s.currentMetadata.LastBackupSize,
		Incremental:    s.currentMetadata.Incremental,
	}
}

// GetLastVersion returns the version of the last backup.
// Returns 0 if no backup has been performed.
func (s *Service) GetLastVersion() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.currentMetadata == nil {
		return 0
	}
	return s.currentMetadata.LastVersion
}

func (s *Service) metadataPath() string {
	return filepath.Join(s.metadataDir, MetadataFileName)
}

func (s *Service) loadMetadata() error {
	data, err := os.ReadFile(s.metadataPath())
	if err != nil {
		return err
	}

	var meta Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return fmt.Errorf("failed to parse metadata: %w", err)
	}

	s.mu.Lock()
	s.currentMetadata = &meta
	s.mu.Unlock()

	return nil
}

func (s *Service) saveMetadata() error {
	s.mu.Lock()
	meta := s.currentMetadata
	s.mu.Unlock()

	if meta == nil {
		return nil
	}

	// Ensure directory exists
	if err := os.MkdirAll(s.metadataDir, 0755); err != nil {
		return fmt.Errorf("failed to create metadata directory: %w", err)
	}

	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if err := os.WriteFile(s.metadataPath(), data, 0644); err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}

	return nil
}

// BackupChunked performs a backup and returns data in chunks.
// This is useful for streaming backups over gRPC.
func (s *Service) BackupChunked(since uint64, compress bool) (<-chan []byte, <-chan error, <-chan uint64) {
	dataCh := make(chan []byte, 10)
	errCh := make(chan error, 1)
	versionCh := make(chan uint64, 1)

	go func() {
		defer close(dataCh)
		defer close(errCh)
		defer close(versionCh)

		var buf bytes.Buffer
		version, err := s.Backup(&buf, since, compress)
		if err != nil {
			errCh <- err
			return
		}

		// Split into chunks
		data := buf.Bytes()
		for i := 0; i < len(data); i += s.chunkSize {
			end := i + s.chunkSize
			if end > len(data) {
				end = len(data)
			}
			dataCh <- data[i:end]
		}

		versionCh <- version
	}()

	return dataCh, errCh, versionCh
}

// RestoreChunked restores from chunks.
// Send chunks to the returned channel, close when done.
func (s *Service) RestoreChunked(compressed bool) (chan<- []byte, <-chan error) {
	dataCh := make(chan []byte, 10)
	errCh := make(chan error, 1)

	go func() {
		defer close(errCh)

		var buf bytes.Buffer
		for chunk := range dataCh {
			buf.Write(chunk)
		}

		if err := s.Restore(&buf, compressed); err != nil {
			errCh <- err
		}
	}()

	return dataCh, errCh
}
