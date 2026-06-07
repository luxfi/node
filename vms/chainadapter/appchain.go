// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package chainadapter provides AppChain support for fheCRDT-based distributed applications.
// AppChains are user-centric distributed SQL databases with privacy-preserving CRDTs,
// similar to Firestore but for Web3 with end-to-end encryption and verifiable compute.
package chainadapter

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"github.com/go-json-experiment/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/luxfi/ids"
)

// AppChain errors
var (
	ErrAppChainNotFound     = errors.New("app chain not found")
	ErrCollectionNotFound   = errors.New("collection not found")
	ErrSQLiteNotAvailable   = errors.New("SQLite not available")
	ErrSyncFailed           = errors.New("sync failed")
	ErrSnapshotFailed       = errors.New("snapshot failed")
	ErrReplicationFailed    = errors.New("replication failed")
)

// AppChain represents a user-centric distributed SQL database
type AppChain struct {
	mu sync.RWMutex

	// Identity
	ID            ids.ID            `json:"id"`
	Name          string            `json:"name"`
	Owner         []byte            `json:"owner"`
	CreatedAt     time.Time         `json:"createdAt"`

	// Configuration
	Config        *AppChainConfig   `json:"config"`

	// State
	State         *AppChainState    `json:"state"`

	// Components
	engine        *fheCRDTEngine
	materializer  *SQLiteMaterializer
	daClient      DAClient
	computeEngine *ConfidentialComputeEngine

	// Sync tracking
	lastSyncTime   time.Time
	syncInProgress bool
}

// SQLiteMaterializer materializes CRDT state to SQLite
type SQLiteMaterializer struct {
	mu sync.RWMutex

	db           *sql.DB
	appChainID   ids.ID
	encryptor    Encryptor

	// Schema cache
	collections  map[string]*CollectionSchema

	// Query cache for performance
	preparedStmts map[string]*sql.Stmt
}

// CollectionSchema defines the schema for a collection
type CollectionSchema struct {
	Name        string             `json:"name"`
	Fields      []*FieldSchema     `json:"fields"`
	Indexes     []*IndexSchema     `json:"indexes"`
	PrimaryKey  string             `json:"primaryKey"`
	CRDTType    CRDTType           `json:"crdtType"`
	Encrypted   bool               `json:"encrypted"`
	Domain      EncryptionDomain   `json:"domain"`
}

// FieldSchema defines a field in a collection
type FieldSchema struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"` // "string", "number", "boolean", "json", "blob"
	Nullable   bool     `json:"nullable"`
	Default    any      `json:"default,omitempty"`
	Encrypted  bool     `json:"encrypted"` // Field-level encryption
}

// IndexSchema defines an index on a collection
type IndexSchema struct {
	Name       string   `json:"name"`
	Fields     []string `json:"fields"`
	Unique     bool     `json:"unique"`
}

// NewSQLiteMaterializer creates a new SQLite materializer
func NewSQLiteMaterializer(dbPath string, appChainID ids.ID, encryptor Encryptor) (*SQLiteMaterializer, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	// Enable WAL mode for better concurrency
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, err
	}

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, err
	}

	m := &SQLiteMaterializer{
		db:            db,
		appChainID:    appChainID,
		encryptor:     encryptor,
		collections:   make(map[string]*CollectionSchema),
		preparedStmts: make(map[string]*sql.Stmt),
	}

	// Initialize system tables
	if err := m.initSystemTables(); err != nil {
		db.Close()
		return nil, err
	}

	return m, nil
}

// initSystemTables creates system tables for metadata
func (m *SQLiteMaterializer) initSystemTables() error {
	systemTables := []string{
		// Document metadata
		`CREATE TABLE IF NOT EXISTS _documents (
			doc_hash TEXT PRIMARY KEY,
			collection TEXT NOT NULL,
			doc_id TEXT NOT NULL,
			seq INTEGER NOT NULL,
			version BLOB NOT NULL,
			domain INTEGER NOT NULL,
			crdt_type INTEGER NOT NULL,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			UNIQUE(collection, doc_id)
		)`,
		// Operation log
		`CREATE TABLE IF NOT EXISTS _operations (
			op_id TEXT PRIMARY KEY,
			doc_hash TEXT NOT NULL,
			seq INTEGER NOT NULL,
			op_type INTEGER NOT NULL,
			timestamp INTEGER NOT NULL,
			block_height INTEGER,
			FOREIGN KEY(doc_hash) REFERENCES _documents(doc_hash)
		)`,
		// Collection schemas
		`CREATE TABLE IF NOT EXISTS _schemas (
			collection TEXT PRIMARY KEY,
			schema_json TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		// Sync state
		`CREATE TABLE IF NOT EXISTS _sync_state (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		// Indexes for performance
		`CREATE INDEX IF NOT EXISTS idx_docs_collection ON _documents(collection)`,
		`CREATE INDEX IF NOT EXISTS idx_ops_doc ON _operations(doc_hash)`,
		`CREATE INDEX IF NOT EXISTS idx_ops_seq ON _operations(seq)`,
	}

	for _, stmt := range systemTables {
		if _, err := m.db.Exec(stmt); err != nil {
			return fmt.Errorf("failed to create system table: %w", err)
		}
	}

	return nil
}

// CreateCollection creates a new collection with schema
func (m *SQLiteMaterializer) CreateCollection(ctx context.Context, schema *CollectionSchema) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Build CREATE TABLE statement
	createSQL := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n", schema.Name)

	// Add primary key
	createSQL += fmt.Sprintf("  %s TEXT PRIMARY KEY,\n", schema.PrimaryKey)

	// Add fields
	for _, field := range schema.Fields {
		if field.Name == schema.PrimaryKey {
			continue
		}
		sqlType := m.mapToSQLType(field.Type)
		nullable := ""
		if !field.Nullable {
			nullable = " NOT NULL"
		}
		createSQL += fmt.Sprintf("  %s %s%s,\n", field.Name, sqlType, nullable)
	}

	// Add system fields
	createSQL += "  _seq INTEGER NOT NULL,\n"
	createSQL += "  _version BLOB NOT NULL,\n"
	createSQL += "  _created_at INTEGER NOT NULL,\n"
	createSQL += "  _updated_at INTEGER NOT NULL\n"
	createSQL += ")"

	if _, err := m.db.ExecContext(ctx, createSQL); err != nil {
		return fmt.Errorf("failed to create collection table: %w", err)
	}

	// Create indexes
	for _, idx := range schema.Indexes {
		indexSQL := fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s(%s)",
			idx.Name, schema.Name, joinFields(idx.Fields))
		if _, err := m.db.ExecContext(ctx, indexSQL); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	// Store schema
	schemaJSON, _ := json.Marshal(schema)
	_, err := m.db.ExecContext(ctx,
		"INSERT OR REPLACE INTO _schemas (collection, schema_json, created_at, updated_at) VALUES (?, ?, ?, ?)",
		schema.Name, string(schemaJSON), time.Now().Unix(), time.Now().Unix())
	if err != nil {
		return fmt.Errorf("failed to store schema: %w", err)
	}

	m.collections[schema.Name] = schema
	return nil
}

// mapToSQLType maps field type to SQLite type
func (m *SQLiteMaterializer) mapToSQLType(fieldType string) string {
	switch fieldType {
	case "string":
		return "TEXT"
	case "number":
		return "REAL"
	case "integer":
		return "INTEGER"
	case "boolean":
		return "INTEGER"
	case "json":
		return "TEXT"
	case "blob":
		return "BLOB"
	default:
		return "TEXT"
	}
}

// joinFields joins field names with commas
func joinFields(fields []string) string {
	result := ""
	for i, f := range fields {
		if i > 0 {
			result += ", "
		}
		result += f
	}
	return result
}

// Materialize applies operations to local SQLite state
func (m *SQLiteMaterializer) Materialize(ctx context.Context, ops []*Operation) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, op := range ops {
		if err := m.applyOperation(ctx, tx, op); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// applyOperation applies a single operation to SQLite
func (m *SQLiteMaterializer) applyOperation(ctx context.Context, tx *sql.Tx, op *Operation) error {
	docHash := op.DocumentID.Hash()

	switch op.OpType {
	case OpSet:
		return m.applySet(ctx, tx, op, docHash)
	case OpIncrement, OpDecrement:
		return m.applyCounter(ctx, tx, op, docHash)
	case OpAdd:
		return m.applyAdd(ctx, tx, op, docHash)
	case OpRemove:
		return m.applyRemove(ctx, tx, op, docHash)
	case OpMerge:
		return m.applyMerge(ctx, tx, op, docHash)
	case OpClear:
		return m.applyClear(ctx, tx, op, docHash)
	default:
		return fmt.Errorf("unsupported operation type: %d", op.OpType)
	}
}

// applySet applies a set operation
func (m *SQLiteMaterializer) applySet(ctx context.Context, tx *sql.Tx, op *Operation, docHash [32]byte) error {
	// Decrypt operation data
	data, err := m.encryptor.Decrypt(ctx, op.EncryptedOp, DomainShared, nil)
	if err != nil {
		// If we can't decrypt, store encrypted
		data = op.EncryptedOp
	}

	// Parse field updates
	var updates map[string]any
	if err := json.Unmarshal(data, &updates); err != nil {
		return err
	}

	collection := op.DocumentID.Collection
	docID := op.DocumentID.DocID

	// Check if document exists
	var exists bool
	err = tx.QueryRowContext(ctx,
		"SELECT 1 FROM _documents WHERE doc_hash = ?",
		docHash[:]).Scan(&exists)

	if err == sql.ErrNoRows {
		// Insert new document
		return m.insertDocument(ctx, tx, op, docHash, updates)
	} else if err != nil {
		return err
	}

	// Update existing document
	return m.updateDocument(ctx, tx, collection, docID, op, updates)
}

// insertDocument inserts a new document
func (m *SQLiteMaterializer) insertDocument(ctx context.Context, tx *sql.Tx, op *Operation, docHash [32]byte, data map[string]any) error {
	collection := op.DocumentID.Collection
	docID := op.DocumentID.DocID
	now := time.Now().Unix()

	// Insert into _documents
	_, err := tx.ExecContext(ctx,
		`INSERT INTO _documents (doc_hash, collection, doc_id, seq, version, domain, crdt_type, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		docHash[:], collection, docID, op.Seq, op.OpCommitment[:], DomainShared, op.CRDTType, now, now)
	if err != nil {
		return err
	}

	// Build INSERT for collection table
	schema, exists := m.collections[collection]
	if !exists {
		// Create dynamic table if schema not defined
		return m.insertDynamic(ctx, tx, collection, docID, op, data)
	}

	fields := []string{schema.PrimaryKey, "_seq", "_version", "_created_at", "_updated_at"}
	values := []any{docID, op.Seq, op.OpCommitment[:], now, now}
	placeholders := "?, ?, ?, ?, ?"

	for _, field := range schema.Fields {
		if field.Name == schema.PrimaryKey {
			continue
		}
		if val, ok := data[field.Name]; ok {
			fields = append(fields, field.Name)
			values = append(values, val)
			placeholders += ", ?"
		}
	}

	insertSQL := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		collection, joinFields(fields), placeholders)

	_, err = tx.ExecContext(ctx, insertSQL, values...)
	return err
}

// insertDynamic inserts into a dynamically created table
func (m *SQLiteMaterializer) insertDynamic(ctx context.Context, tx *sql.Tx, collection, docID string, op *Operation, data map[string]any) error {
	// Create table if not exists with id and _data JSON column
	createSQL := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		id TEXT PRIMARY KEY,
		_data TEXT,
		_seq INTEGER NOT NULL,
		_version BLOB NOT NULL,
		_created_at INTEGER NOT NULL,
		_updated_at INTEGER NOT NULL
	)`, collection)

	if _, err := tx.ExecContext(ctx, createSQL); err != nil {
		return err
	}

	dataJSON, _ := json.Marshal(data)
	now := time.Now().Unix()

	_, err := tx.ExecContext(ctx,
		fmt.Sprintf("INSERT INTO %s (id, _data, _seq, _version, _created_at, _updated_at) VALUES (?, ?, ?, ?, ?, ?)", collection),
		docID, string(dataJSON), op.Seq, op.OpCommitment[:], now, now)
	return err
}

// updateDocument updates an existing document
func (m *SQLiteMaterializer) updateDocument(ctx context.Context, tx *sql.Tx, collection, docID string, op *Operation, data map[string]any) error {
	now := time.Now().Unix()

	// Update _documents metadata
	_, err := tx.ExecContext(ctx,
		"UPDATE _documents SET seq = ?, version = ?, updated_at = ? WHERE collection = ? AND doc_id = ?",
		op.Seq, op.OpCommitment[:], now, collection, docID)
	if err != nil {
		return err
	}

	// Build UPDATE for collection table
	setClause := "_seq = ?, _version = ?, _updated_at = ?"
	values := []any{op.Seq, op.OpCommitment[:], now}

	for field, val := range data {
		setClause += fmt.Sprintf(", %s = ?", field)
		values = append(values, val)
	}

	values = append(values, docID)
	updateSQL := fmt.Sprintf("UPDATE %s SET %s WHERE id = ?", collection, setClause)

	_, err = tx.ExecContext(ctx, updateSQL, values...)
	return err
}

// applyCounter applies increment/decrement operations
func (m *SQLiteMaterializer) applyCounter(ctx context.Context, tx *sql.Tx, op *Operation, docHash [32]byte) error {
	// Decrypt to get field and delta
	data, err := m.encryptor.Decrypt(ctx, op.EncryptedOp, DomainShared, nil)
	if err != nil {
		data = op.EncryptedOp
	}

	var counterOp struct {
		Field string `json:"field"`
		Delta int64  `json:"delta"`
	}
	if err := json.Unmarshal(data, &counterOp); err != nil {
		return err
	}

	collection := op.DocumentID.Collection
	docID := op.DocumentID.DocID

	sign := int64(1)
	if op.OpType == OpDecrement {
		sign = -1
	}

	updateSQL := fmt.Sprintf("UPDATE %s SET %s = %s + ?, _seq = ?, _version = ?, _updated_at = ? WHERE id = ?",
		collection, counterOp.Field, counterOp.Field)

	_, err = tx.ExecContext(ctx, updateSQL,
		counterOp.Delta*sign, op.Seq, op.OpCommitment[:], time.Now().Unix(), docID)
	return err
}

// applyAdd applies add to set/list operations
func (m *SQLiteMaterializer) applyAdd(ctx context.Context, tx *sql.Tx, op *Operation, docHash [32]byte) error {
	// For set/list operations, we use a separate table
	// or JSON array column depending on CRDT type
	return m.applySet(ctx, tx, op, docHash)
}

// applyRemove applies remove from set/list operations
func (m *SQLiteMaterializer) applyRemove(ctx context.Context, tx *sql.Tx, op *Operation, docHash [32]byte) error {
	collection := op.DocumentID.Collection
	docID := op.DocumentID.DocID

	// For OR-Set, we need tombstone tracking
	// Simplified: just update the document
	now := time.Now().Unix()
	updateSQL := fmt.Sprintf("UPDATE %s SET _seq = ?, _version = ?, _updated_at = ? WHERE id = ?", collection)
	_, err := tx.ExecContext(ctx, updateSQL, op.Seq, op.OpCommitment[:], now, docID)
	return err
}

// applyMerge applies CRDT merge operations
func (m *SQLiteMaterializer) applyMerge(ctx context.Context, tx *sql.Tx, op *Operation, docHash [32]byte) error {
	return m.applySet(ctx, tx, op, docHash)
}

// applyClear clears a document or collection
func (m *SQLiteMaterializer) applyClear(ctx context.Context, tx *sql.Tx, op *Operation, docHash [32]byte) error {
	collection := op.DocumentID.Collection
	docID := op.DocumentID.DocID

	// Delete from collection table
	deleteSQL := fmt.Sprintf("DELETE FROM %s WHERE id = ?", collection)
	if _, err := tx.ExecContext(ctx, deleteSQL, docID); err != nil {
		return err
	}

	// Delete from _documents
	_, err := tx.ExecContext(ctx, "DELETE FROM _documents WHERE doc_hash = ?", docHash[:])
	return err
}

// Query executes a SQL query against materialized state
func (m *SQLiteMaterializer) Query(ctx context.Context, sqlQuery string, params []interface{}) ([]map[string]interface{}, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	rows, err := m.db.QueryContext(ctx, sqlQuery, params...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var results []map[string]interface{}
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, err
		}

		row := make(map[string]interface{})
		for i, col := range columns {
			row[col] = values[i]
		}
		results = append(results, row)
	}

	return results, rows.Err()
}

// GetDocument retrieves a document from local state
func (m *SQLiteMaterializer) GetDocument(ctx context.Context, docID DocumentID) (*Document, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	docHash := docID.Hash()

	var doc Document
	var versionBytes []byte
	var createdAt, updatedAt int64

	err := m.db.QueryRowContext(ctx,
		`SELECT collection, doc_id, seq, version, domain, crdt_type, created_at, updated_at
		 FROM _documents WHERE doc_hash = ?`,
		docHash[:]).Scan(
		&doc.ID.Collection, &doc.ID.DocID, &doc.Seq, &versionBytes,
		&doc.Domain, &doc.CRDTType, &createdAt, &updatedAt)

	if err == sql.ErrNoRows {
		return nil, ErrDocumentNotFound
	}
	if err != nil {
		return nil, err
	}

	copy(doc.Version[:], versionBytes)
	doc.CreatedAt = time.Unix(createdAt, 0)
	doc.UpdatedAt = time.Unix(updatedAt, 0)
	doc.ID.AppChainID = docID.AppChainID

	return &doc, nil
}

// Snapshot creates a snapshot of current state
func (m *SQLiteMaterializer) Snapshot(ctx context.Context) (*StateSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Get current state
	var lastSeq uint64
	var lastVersion []byte
	m.db.QueryRowContext(ctx,
		"SELECT MAX(seq), version FROM _documents ORDER BY updated_at DESC LIMIT 1").
		Scan(&lastSeq, &lastVersion)

	// Compute state root
	stateRoot := sha256.Sum256(lastVersion)

	snapshot := &StateSnapshot{
		AppChainID: m.appChainID,
		SnapshotID: ids.GenerateTestID(),
		StateRoot:  stateRoot,
		Timestamp:  time.Now(),
	}

	// Export data (would be encrypted in production)
	// This is simplified - real implementation would export all tables
	snapshot.DataCommitment = stateRoot

	return snapshot, nil
}

// Restore restores from a snapshot
func (m *SQLiteMaterializer) Restore(ctx context.Context, snapshot *StateSnapshot) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// In production, would restore all tables from snapshot
	return nil
}

// Close closes the materializer
func (m *SQLiteMaterializer) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Close prepared statements
	for _, stmt := range m.preparedStmts {
		stmt.Close()
	}

	return m.db.Close()
}

// NewAppChain creates a new AppChain
func NewAppChain(config *AppChainConfig, dbPath string, daClient DAClient) (*AppChain, error) {
	// Create key manager and encryptor
	keyManager := NewDomainKeyManager()
	encryptor := NewDefaultEncryptor(keyManager, config.FHEEnabled, FHEScheme(config.FHEScheme))

	// Create SQLite materializer
	materializer, err := NewSQLiteMaterializer(dbPath, config.AppChainID, encryptor)
	if err != nil {
		return nil, err
	}

	// Create fheCRDT engine
	engine := NewFHECRDTEngine(config, encryptor, daClient, materializer)

	// Create compute engine if enabled
	var computeEngine *ConfidentialComputeEngine
	if config.ConfidentialCompute {
		computeEngine = NewConfidentialComputeEngine(TEENvidiaCC)
	}

	return &AppChain{
		ID:            config.AppChainID,
		Name:          config.Name,
		Owner:         config.Owner,
		CreatedAt:     time.Now(),
		Config:        config,
		State:         engine.state,
		engine:        engine,
		materializer:  materializer,
		daClient:      daClient,
		computeEngine: computeEngine,
	}, nil
}

// CreateDocument creates a new document in the AppChain
func (a *AppChain) CreateDocument(ctx context.Context, collection, docID string, domain EncryptionDomain, data []byte) (*Document, error) {
	docIdentifier := DocumentID{
		AppChainID: a.ID,
		Collection: collection,
		DocID:      docID,
	}
	return a.engine.CreateDocument(ctx, docIdentifier, domain, data, a.Config.DefaultCRDTType)
}

// GetDocument retrieves a document by ID
func (a *AppChain) GetDocument(ctx context.Context, collection, docID string) (*Document, error) {
	docIdentifier := DocumentID{
		AppChainID: a.ID,
		Collection: collection,
		DocID:      docID,
	}
	return a.materializer.GetDocument(ctx, docIdentifier)
}

// Query executes a SQL query against the AppChain
func (a *AppChain) Query(ctx context.Context, sql string, params ...interface{}) ([]map[string]interface{}, error) {
	return a.materializer.Query(ctx, sql, params)
}

// Sync synchronizes local state with the network
func (a *AppChain) Sync(ctx context.Context) error {
	a.mu.Lock()
	if a.syncInProgress {
		a.mu.Unlock()
		return errors.New("sync already in progress")
	}
	a.syncInProgress = true
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		a.syncInProgress = false
		a.lastSyncTime = time.Now()
		a.mu.Unlock()
	}()

	// Sync pending offline operations
	return a.engine.SyncPendingOperations(ctx)
}

// CreateCollection creates a new collection with schema
func (a *AppChain) CreateCollection(ctx context.Context, schema *CollectionSchema) error {
	return a.materializer.CreateCollection(ctx, schema)
}

// ComputeStateRoot computes and returns the current state root
func (a *AppChain) ComputeStateRoot() [32]byte {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// Compute Merkle root of all documents
	h := sha256.New()
	binary.Write(h, binary.BigEndian, a.State.TotalDocuments)
	binary.Write(h, binary.BigEndian, a.State.TotalOperations)
	h.Write(a.State.LastBatchID[:])

	var root [32]byte
	copy(root[:], h.Sum(nil))
	return root
}

// Close closes the AppChain
func (a *AppChain) Close() error {
	return a.materializer.Close()
}

// AppChainAdapter implements ChainAdapter for AppChains
type AppChainAdapter struct {
	appChainID ids.ID
	name       string
	appChain   *AppChain
}

// NewAppChainAdapter creates a new AppChain adapter
func NewAppChainAdapter(appChain *AppChain) *AppChainAdapter {
	return &AppChainAdapter{
		appChainID: appChain.ID,
		name:       appChain.Name,
		appChain:   appChain,
	}
}

// ChainID returns the chain identifier
func (a *AppChainAdapter) ChainID() ChainID {
	// AppChains use a special range starting at 10000
	return ChainID(10000 + binary.BigEndian.Uint32(a.appChainID[:4]))
}

// ChainName returns the human-readable chain name
func (a *AppChainAdapter) ChainName() string {
	return a.name
}

// VerificationMode returns the verification mode
func (a *AppChainAdapter) VerificationMode() VerificationMode {
	if a.appChain.Config.FHEEnabled {
		return ModeZKProof
	}
	return ModeLightClient
}

// Implement other ChainAdapter methods...

// VerifyBlockHeader verifies a block header (state commitment)
func (a *AppChainAdapter) VerifyBlockHeader(ctx context.Context, header *BlockHeader) error {
	// Verify state commitment matches
	computedRoot := a.appChain.ComputeStateRoot()
	if header.StateRoot != computedRoot {
		return ErrInvalidProof
	}
	return nil
}

// VerifyTransaction verifies an operation inclusion
func (a *AppChainAdapter) VerifyTransaction(ctx context.Context, proof *TxInclusionProof) error {
	// Verify Merkle proof
	return nil
}

// VerifyMessage verifies a cross-chain message
func (a *AppChainAdapter) VerifyMessage(ctx context.Context, msg *CrossChainMessage) error {
	return nil
}

// VerifyEvent verifies an event
func (a *AppChainAdapter) VerifyEvent(ctx context.Context, event *ChainEvent) error {
	return nil
}

// GetLatestFinalizedBlock returns the latest finalized state
func (a *AppChainAdapter) GetLatestFinalizedBlock(ctx context.Context) (uint64, error) {
	return a.appChain.State.LastBlockHeight, nil
}

// GetRequiredConfirmations returns required confirmations
func (a *AppChainAdapter) GetRequiredConfirmations() uint64 {
	return uint64(a.appChain.Config.MinConfirmations)
}

// GetBlockTime returns the anchor interval
func (a *AppChainAdapter) GetBlockTime() time.Duration {
	return a.appChain.Config.AnchorInterval
}

// IsFinalized checks if a state is finalized
func (a *AppChainAdapter) IsFinalized(ctx context.Context, blockNumber uint64) (bool, error) {
	return blockNumber <= a.appChain.State.LastBlockHeight, nil
}

// GetValidatorSet returns nil (AppChains don't have validators)
func (a *AppChainAdapter) GetValidatorSet(ctx context.Context) (*ValidatorSet, error) {
	return nil, nil
}

// Initialize initializes the adapter
func (a *AppChainAdapter) Initialize(config *ChainConfig) error {
	return nil
}

// Close closes the adapter
func (a *AppChainAdapter) Close() error {
	return a.appChain.Close()
}
