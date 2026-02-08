package knowledge

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // Pure Go SQLite driver
)

// Store defines the interface for interacting with the Knowledge Graph
type Store interface {
	Close() error
	// Schema Management
	RegisterSchema(ctx context.Context, schema Schema) error
	RegisterBuiltinSchema(ctx context.Context, schema Schema) error
	ListSchemas(ctx context.Context) ([]Schema, error)
	GetSchema(ctx context.Context, name string) (*Schema, error)
	UpdateSchemaServices(ctx context.Context, name string, services []string) error

	// Ingestion
	IngestNodes(ctx context.Context, nodes []Node) error
	IngestEdges(ctx context.Context, edges []Edge) error
	IngestStatistics(ctx context.Context, stats []Statistic) error
	IngestEvents(ctx context.Context, events []Event) error

	// Notes
	CreateNote(ctx context.Context, title, body string, nodeIDs []string, edgeRefs []EdgeRef) (*Note, error)
	GetNote(ctx context.Context, id string) (*Note, []string, []EdgeRef, error)
	DeleteNote(ctx context.Context, id string) error

	// Retrieval
	Search(ctx context.Context, query string, limit int) (*SearchResult, error)
	GetTopology(ctx context.Context, rootID string, depth int) (*Topology, error)
}

type SQLiteStore struct {
	db *sql.DB
}

// NewStore initializes the SQLite database
func NewStore(dbPath string) (Store, error) {
	if dbPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		dir := filepath.Join(home, ".last9")
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory: %w", err)
		}
		dbPath = filepath.Join(dir, "knowledge.db")
	}

	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	s := &SQLiteStore{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return s, nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) migrate() error {
	// Phase 1: Base tables (includes env column for new databases)
	baseTables := []string{
		// Nodes
		`CREATE TABLE IF NOT EXISTS nodes (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			name TEXT,
			env TEXT,
			properties JSON,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE INDEX IF NOT EXISTS idx_nodes_type ON nodes(type);`,

		// Edges
		`CREATE TABLE IF NOT EXISTS edges (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			source_id TEXT NOT NULL,
			target_id TEXT NOT NULL,
			relation TEXT NOT NULL,
			properties JSON,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(source_id) REFERENCES nodes(id),
			FOREIGN KEY(target_id) REFERENCES nodes(id),
			UNIQUE(source_id, target_id, relation)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_edges_source ON edges(source_id);`,
		`CREATE INDEX IF NOT EXISTS idx_edges_target ON edges(target_id);`,

		// Schemas
		`CREATE TABLE IF NOT EXISTS schemas (
			name TEXT PRIMARY KEY,
			definition JSON NOT NULL,
			scope_environments JSON,
			scope_services JSON,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,

		// Statistics
		`CREATE TABLE IF NOT EXISTS statistics (
			node_id TEXT NOT NULL,
			metric_name TEXT NOT NULL,
			value REAL,
			unit TEXT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY(node_id, metric_name),
			FOREIGN KEY(node_id) REFERENCES nodes(id)
		);`,

		// Events
		`CREATE TABLE IF NOT EXISTS events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			source_id TEXT NOT NULL,
			target_id TEXT,
			type TEXT NOT NULL,
			status TEXT,
			severity TEXT,
			time_window_start DATETIME NOT NULL,
			time_window_end DATETIME NOT NULL,
			recent_timestamp DATETIME NOT NULL,
			count INTEGER DEFAULT 1,
			metadata JSON,
			FOREIGN KEY(source_id) REFERENCES nodes(id),
			FOREIGN KEY(target_id) REFERENCES nodes(id),
			UNIQUE(source_id, target_id, type, status, severity, time_window_start)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_events_lookup ON events(source_id, time_window_start);`,

		// Notes table
		`CREATE TABLE IF NOT EXISTS notes (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			body TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		// Link tables for note ↔ node and note ↔ edge associations
		`CREATE TABLE IF NOT EXISTS note_node_links (
			note_id TEXT NOT NULL,
			node_id TEXT NOT NULL,
			PRIMARY KEY(note_id, node_id),
			FOREIGN KEY(note_id) REFERENCES notes(id) ON DELETE CASCADE,
			FOREIGN KEY(node_id) REFERENCES nodes(id)
		);`,
		`CREATE TABLE IF NOT EXISTS note_edge_links (
			note_id TEXT NOT NULL,
			edge_source_id TEXT NOT NULL,
			edge_target_id TEXT NOT NULL,
			edge_relation TEXT NOT NULL,
			PRIMARY KEY(note_id, edge_source_id, edge_target_id, edge_relation),
			FOREIGN KEY(note_id) REFERENCES notes(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_note_node_links_node ON note_node_links(node_id);`,
		`CREATE INDEX IF NOT EXISTS idx_note_edge_links_edge ON note_edge_links(edge_source_id, edge_target_id, edge_relation);`,
	}

	for _, q := range baseTables {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("base table query failed: %s, err: %w", q, err)
		}
	}

	// Phase 2: Column migrations for existing databases.
	// Must run before FTS so that new columns exist when FTS references them.
	columnMigrations := []struct {
		table, column, colDef string
	}{
		{"schemas", "description", "TEXT DEFAULT ''"},
		{"schemas", "builtin", "INTEGER DEFAULT 0"},
		{"nodes", "env", "TEXT"},
	}
	for _, m := range columnMigrations {
		if err := s.addColumnIfNotExists(m.table, m.column, m.colDef); err != nil {
			return fmt.Errorf("column migration failed (%s.%s): %w", m.table, m.column, err)
		}
	}

	// Phase 3: FTS tables — drop and recreate so column set stays in sync.
	ftsQueries := []string{
		// Drop old FTS infrastructure so we can recreate as external-content table.
		`DROP TRIGGER IF EXISTS nodes_ai;`,
		`DROP TRIGGER IF EXISTS nodes_ad;`,
		`DROP TRIGGER IF EXISTS nodes_au;`,
		`DROP TABLE IF EXISTS nodes_fts;`,

		// External-content FTS5 table referencing nodes via rowid (includes env)
		`CREATE VIRTUAL TABLE IF NOT EXISTS nodes_fts USING fts5(
			id, name, type, properties, env,
			content=nodes, content_rowid=rowid
		);`,
		`CREATE TRIGGER IF NOT EXISTS nodes_ai AFTER INSERT ON nodes BEGIN
			INSERT INTO nodes_fts(rowid, id, name, type, properties, env)
			VALUES (new.rowid, new.id, new.name, new.type, new.properties, new.env);
		END;`,
		`CREATE TRIGGER IF NOT EXISTS nodes_ad AFTER DELETE ON nodes BEGIN
			INSERT INTO nodes_fts(nodes_fts, rowid, id, name, type, properties, env)
			VALUES('delete', old.rowid, old.id, old.name, old.type, old.properties, old.env);
		END;`,
		`CREATE TRIGGER IF NOT EXISTS nodes_au AFTER UPDATE ON nodes BEGIN
			INSERT INTO nodes_fts(nodes_fts, rowid, id, name, type, properties, env)
			VALUES('delete', old.rowid, old.id, old.name, old.type, old.properties, old.env);
			INSERT INTO nodes_fts(rowid, id, name, type, properties, env)
			VALUES (new.rowid, new.id, new.name, new.type, new.properties, new.env);
		END;`,

		// Rebuild FTS index from existing node data
		`INSERT INTO nodes_fts(nodes_fts) VALUES('rebuild');`,

		// Drop old notes FTS infrastructure so we can recreate cleanly
		`DROP TRIGGER IF EXISTS notes_ai;`,
		`DROP TRIGGER IF EXISTS notes_ad;`,
		`DROP TRIGGER IF EXISTS notes_au;`,
		`DROP TABLE IF EXISTS notes_fts;`,

		// External-content FTS5 table for notes
		`CREATE VIRTUAL TABLE IF NOT EXISTS notes_fts USING fts5(
			title, body,
			content=notes, content_rowid=rowid
		);`,
		`CREATE TRIGGER IF NOT EXISTS notes_ai AFTER INSERT ON notes BEGIN
			INSERT INTO notes_fts(rowid, title, body)
			VALUES (new.rowid, new.title, new.body);
		END;`,
		`CREATE TRIGGER IF NOT EXISTS notes_ad AFTER DELETE ON notes BEGIN
			INSERT INTO notes_fts(notes_fts, rowid, title, body)
			VALUES('delete', old.rowid, old.title, old.body);
		END;`,
		`CREATE TRIGGER IF NOT EXISTS notes_au AFTER UPDATE ON notes BEGIN
			INSERT INTO notes_fts(notes_fts, rowid, title, body)
			VALUES('delete', old.rowid, old.title, old.body);
			INSERT INTO notes_fts(rowid, title, body)
			VALUES (new.rowid, new.title, new.body);
		END;`,

		// Rebuild notes FTS index from existing data
		`INSERT INTO notes_fts(notes_fts) VALUES('rebuild');`,
	}

	for _, q := range ftsQueries {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("FTS query failed: %s, err: %w", q, err)
		}
	}

	return nil
}

// addColumnIfNotExists adds a column to a table if it doesn't already exist.
// SQLite lacks ALTER TABLE ... ADD COLUMN IF NOT EXISTS, so we check PRAGMA table_info.
func (s *SQLiteStore) addColumnIfNotExists(table, column, colDef string) error {
	rows, err := s.db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return err
		}
		if name == column {
			return nil // column already exists
		}
	}

	_, err = s.db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, colDef))
	return err
}

// nullableString converts an empty string to nil so that SQL COALESCE
// preserves an existing non-NULL value when the new value is unset.
func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// IngestNodes upserts nodes
func (s *SQLiteStore) IngestNodes(ctx context.Context, nodes []Node) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO nodes (id, type, name, env, properties, updated_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET
			type=excluded.type,
			name=excluded.name,
			env=COALESCE(excluded.env, nodes.env),
			properties=excluded.properties,
			updated_at=CURRENT_TIMESTAMP
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, n := range nodes {
		props, _ := json.Marshal(n.Properties)
		if _, err := stmt.ExecContext(ctx, n.ID, n.Type, n.Name, nullableString(n.Env), string(props)); err != nil {
			return fmt.Errorf("failed to ingest node %s: %w", n.ID, err)
		}
	}
	return tx.Commit()
}

// IngestEdges upserts edges
func (s *SQLiteStore) IngestEdges(ctx context.Context, edges []Edge) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO edges (source_id, target_id, relation, properties, updated_at) 
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(source_id, target_id, relation) DO UPDATE SET 
			properties=excluded.properties, 
			updated_at=CURRENT_TIMESTAMP
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, e := range edges {
		props, _ := json.Marshal(e.Properties)
		if _, err := stmt.ExecContext(ctx, e.SourceID, e.TargetID, e.Relation, string(props)); err != nil {
			return fmt.Errorf("failed to ingest edge %s->%s: %w", e.SourceID, e.TargetID, err)
		}
	}
	return tx.Commit()
}

// IngestEvents aggregates events into buckets
func (s *SQLiteStore) IngestEvents(ctx context.Context, events []Event) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 5-minute bucket
	bucketSize := 5 * time.Minute

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO events (source_id, target_id, type, status, severity, time_window_start, time_window_end, recent_timestamp, count, metadata)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?)
		ON CONFLICT(source_id, target_id, type, status, severity, time_window_start) DO UPDATE SET
			count = events.count + 1,
			recent_timestamp = excluded.recent_timestamp,
			metadata = excluded.metadata
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, e := range events {
		// Calculate bucket
		ts := e.Timestamp
		if ts.IsZero() {
			ts = time.Now()
		}
		bucketStart := ts.Truncate(bucketSize)
		bucketEnd := bucketStart.Add(bucketSize)

		meta, _ := json.Marshal(e.Metadata)

		// Create nullable targetID
		var targetID sql.NullString
		if e.TargetID != "" {
			targetID.String = e.TargetID
			targetID.Valid = true
		}

		if _, err := stmt.ExecContext(ctx, e.SourceID, targetID, e.Type, e.Status, e.Severity, bucketStart, bucketEnd, ts, string(meta)); err != nil {
			return fmt.Errorf("failed to ingest event: %w", err)
		}
	}
	return tx.Commit()
}

// IngestStatistics upserts stats
func (s *SQLiteStore) IngestStatistics(ctx context.Context, stats []Statistic) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO statistics (node_id, metric_name, value, unit, timestamp) 
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(node_id, metric_name) DO UPDATE SET 
			value=excluded.value, 
			unit=excluded.unit, 
			timestamp=excluded.timestamp
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, stat := range stats {
		if _, err := stmt.ExecContext(ctx, stat.NodeID, stat.MetricName, stat.Value, stat.Unit, stat.Timestamp); err != nil {
			return fmt.Errorf("failed to ingest stat for %s: %w", stat.NodeID, err)
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) RegisterSchema(ctx context.Context, schema Schema) error {
	def, _ := json.Marshal(schema.Blueprint)
	envs, _ := json.Marshal(schema.Environments)
	svcs, _ := json.Marshal(schema.Services)

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO schemas (name, definition, scope_environments, scope_services, description, builtin)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			definition=excluded.definition,
			scope_environments=excluded.scope_environments,
			scope_services=excluded.scope_services,
			description=excluded.description,
			builtin=excluded.builtin
	`, schema.Name, string(def), string(envs), string(svcs), schema.Description, boolToInt(schema.Builtin))
	return err
}

// RegisterBuiltinSchema upserts the blueprint, description, and builtin flag
// but preserves user-assigned scope_services so restarts don't erase associations.
func (s *SQLiteStore) RegisterBuiltinSchema(ctx context.Context, schema Schema) error {
	def, _ := json.Marshal(schema.Blueprint)
	envs, _ := json.Marshal(schema.Environments)
	svcs, _ := json.Marshal(schema.Services)

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO schemas (name, definition, scope_environments, scope_services, description, builtin)
		VALUES (?, ?, ?, ?, ?, 1)
		ON CONFLICT(name) DO UPDATE SET
			definition=excluded.definition,
			scope_environments=excluded.scope_environments,
			description=excluded.description,
			builtin=1
	`, schema.Name, string(def), string(envs), string(svcs), schema.Description)
	return err
}

// UpdateSchemaServices replaces the scope_services list for a given schema.
func (s *SQLiteStore) UpdateSchemaServices(ctx context.Context, name string, services []string) error {
	svcs, _ := json.Marshal(services)
	result, err := s.db.ExecContext(ctx, `UPDATE schemas SET scope_services = ? WHERE name = ?`, string(svcs), name)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("schema '%s' not found", name)
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *SQLiteStore) ListSchemas(ctx context.Context) ([]Schema, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, definition, scope_environments, scope_services, description, builtin FROM schemas`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var schemas []Schema
	for rows.Next() {
		var sh Schema
		var def, envs, svcs []byte
		var builtinInt int
		if err := rows.Scan(&sh.Name, &def, &envs, &svcs, &sh.Description, &builtinInt); err != nil {
			return nil, err
		}
		json.Unmarshal(def, &sh.Blueprint)
		json.Unmarshal(envs, &sh.Environments)
		json.Unmarshal(svcs, &sh.Services)
		sh.Builtin = builtinInt == 1
		schemas = append(schemas, sh)
	}
	return schemas, nil
}

func (s *SQLiteStore) GetSchema(ctx context.Context, name string) (*Schema, error) {
	row := s.db.QueryRowContext(ctx, `SELECT name, definition, scope_environments, scope_services, description, builtin FROM schemas WHERE name = ?`, name)
	var sh Schema
	var def, envs, svcs []byte
	var builtinInt int
	if err := row.Scan(&sh.Name, &def, &envs, &svcs, &sh.Description, &builtinInt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // Not found
		}
		return nil, err
	}
	json.Unmarshal(def, &sh.Blueprint)
	json.Unmarshal(envs, &sh.Environments)
	json.Unmarshal(svcs, &sh.Services)
	sh.Builtin = builtinInt == 1
	return &sh, nil
}

// Search implements full text search across nodes and notes.
func (s *SQLiteStore) Search(ctx context.Context, query string, limit int) (*SearchResult, error) {
	// 1. Search Nodes via FTS (JOIN with nodes since external-content FTS
	// tables don't reliably return column values directly)
	rows, err := s.db.QueryContext(ctx, `
		SELECT n.id, n.name, n.type, n.properties, n.env
		FROM nodes_fts fts
		JOIN nodes n ON n.rowid = fts.rowid
		WHERE nodes_fts MATCH ?
		ORDER BY fts.rank
		LIMIT ?`, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := &SearchResult{
		Nodes:  []Node{},
		Edges:  []Edge{},
		Stats:  []Statistic{},
		Events: []Event{},
		Notes:  []NoteRef{},
	}

	nodeIDs := []interface{}{}
	for rows.Next() {
		var n Node
		var props []byte
		var env sql.NullString
		if err := rows.Scan(&n.ID, &n.Name, &n.Type, &props, &env); err != nil {
			continue
		}
		json.Unmarshal(props, &n.Properties)
		if env.Valid {
			n.Env = env.String
		}
		result.Nodes = append(result.Nodes, n)
		nodeIDs = append(nodeIDs, n.ID)
	}

	// 2. Search Notes via FTS — always runs, even when no nodes match
	var noteRefs []NoteRef
	noteRows, err := s.db.QueryContext(ctx, `
		SELECT n.id, n.title
		FROM notes_fts fts
		JOIN notes n ON n.rowid = fts.rowid
		WHERE notes_fts MATCH ?
		LIMIT ?`, query, limit)
	if err == nil {
		defer noteRows.Close()
		for noteRows.Next() {
			var ref NoteRef
			if err := noteRows.Scan(&ref.ID, &ref.Title); err != nil {
				continue
			}
			noteRefs = append(noteRefs, ref)
		}
	}

	// 3. Node-dependent queries: edges, stats, events, linked notes
	if len(nodeIDs) > 0 {
		placeholders := strings.Repeat("?,", len(nodeIDs)-1) + "?"

		// Edges (Outgoing and Incoming)
		edgeQuery := fmt.Sprintf(`
			SELECT source_id, target_id, relation, properties
			FROM edges
			WHERE source_id IN (%s) OR target_id IN (%s)`, placeholders, placeholders)

		args := append(nodeIDs, nodeIDs...)
		edgeRows, err := s.db.QueryContext(ctx, edgeQuery, args...)
		if err == nil {
			defer edgeRows.Close()
			for edgeRows.Next() {
				var e Edge
				var props []byte
				edgeRows.Scan(&e.SourceID, &e.TargetID, &e.Relation, &props)
				json.Unmarshal(props, &e.Properties)
				result.Edges = append(result.Edges, e)
			}
		}

		// Stats
		statQuery := fmt.Sprintf(`SELECT node_id, metric_name, value, unit, timestamp FROM statistics WHERE node_id IN (%s)`, placeholders)
		statRows, err := s.db.QueryContext(ctx, statQuery, nodeIDs...)
		if err == nil {
			defer statRows.Close()
			for statRows.Next() {
				var st Statistic
				statRows.Scan(&st.NodeID, &st.MetricName, &st.Value, &st.Unit, &st.Timestamp)
				result.Stats = append(result.Stats, st)
			}
		}

		// Recent High Severity Events (Last 1 hour)
		eventQuery := fmt.Sprintf(`
			SELECT source_id, target_id, type, status, severity, recent_timestamp, count, metadata
			FROM events
			WHERE (source_id IN (%s) OR target_id IN (%s))
			AND recent_timestamp > datetime('now', '-1 hour')
			AND (severity = 'error' OR severity = 'fatal')
			ORDER BY recent_timestamp DESC LIMIT 10`, placeholders, placeholders)

		eventRows, err := s.db.QueryContext(ctx, eventQuery, args...)
		if err == nil {
			defer eventRows.Close()
			for eventRows.Next() {
				var ev Event
				var targetID sql.NullString
				var meta []byte
				eventRows.Scan(&ev.SourceID, &targetID, &ev.Type, &ev.Status, &ev.Severity, &ev.Timestamp, &ev.Count, &meta)
				if targetID.Valid {
					ev.TargetID = targetID.String
				}
				json.Unmarshal(meta, &ev.Metadata)
				result.Events = append(result.Events, ev)
			}
		}

		// Notes linked to matched nodes (directly or via edges involving those nodes)
		linkedNoteQuery := fmt.Sprintf(`
			SELECT DISTINCT n.id, n.title FROM note_node_links lnk
			JOIN notes n ON n.id = lnk.note_id
			WHERE lnk.node_id IN (%s)
			UNION
			SELECT DISTINCT n.id, n.title FROM note_edge_links elk
			JOIN notes n ON n.id = elk.note_id
			WHERE elk.edge_source_id IN (%s) OR elk.edge_target_id IN (%s)`,
			placeholders, placeholders, placeholders)
		linkedNoteArgs := make([]interface{}, 0, len(nodeIDs)*3)
		linkedNoteArgs = append(linkedNoteArgs, nodeIDs...)
		linkedNoteArgs = append(linkedNoteArgs, nodeIDs...)
		linkedNoteArgs = append(linkedNoteArgs, nodeIDs...)
		linkedNoteRows, err := s.db.QueryContext(ctx, linkedNoteQuery, linkedNoteArgs...)
		if err == nil {
			defer linkedNoteRows.Close()
			for linkedNoteRows.Next() {
				var ref NoteRef
				if err := linkedNoteRows.Scan(&ref.ID, &ref.Title); err != nil {
					continue
				}
				noteRefs = append(noteRefs, ref)
			}
		}
	}

	// 4. Deduplicate and assign notes
	result.Notes = deduplicateNoteRefs(noteRefs)

	return result, nil
}

func (s *SQLiteStore) GetTopology(ctx context.Context, rootID string, depth int) (*Topology, error) {
	// Recursive CTE to traverse graph
	query := `
	WITH RECURSIVE topology(source_id, target_id, relation, depth) AS (
		SELECT source_id, target_id, relation, 1
		FROM edges
		WHERE source_id = ?
		UNION ALL
		SELECT e.source_id, e.target_id, e.relation, t.depth + 1
		FROM edges e
		JOIN topology t ON e.source_id = t.target_id
		WHERE t.depth < ?
	)
	SELECT source_id, target_id, relation FROM topology;
	`
	rows, err := s.db.QueryContext(ctx, query, rootID, depth)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	topo := &Topology{RootID: rootID, Edges: []Edge{}}
	for rows.Next() {
		var e Edge
		rows.Scan(&e.SourceID, &e.TargetID, &e.Relation)
		topo.Edges = append(topo.Edges, e)
	}
	return topo, nil
}

func generateNoteID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "note_" + hex.EncodeToString(b)
}

// CreateNote inserts a note and its links to nodes and edges in a single transaction.
func (s *SQLiteStore) CreateNote(ctx context.Context, title, body string, nodeIDs []string, edgeRefs []EdgeRef) (*Note, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	id := generateNoteID()
	var createdAt time.Time
	err = tx.QueryRowContext(ctx, `
		INSERT INTO notes (id, title, body) VALUES (?, ?, ?)
		RETURNING created_at`, id, title, body).Scan(&createdAt)
	if err != nil {
		return nil, fmt.Errorf("failed to insert note: %w", err)
	}

	// Link to nodes
	if len(nodeIDs) > 0 {
		nodeStmt, err := tx.PrepareContext(ctx, `INSERT INTO note_node_links (note_id, node_id) VALUES (?, ?)`)
		if err != nil {
			return nil, err
		}
		defer nodeStmt.Close()
		for _, nid := range nodeIDs {
			if _, err := nodeStmt.ExecContext(ctx, id, nid); err != nil {
				return nil, fmt.Errorf("failed to link note to node %s: %w", nid, err)
			}
		}
	}

	// Link to edges
	if len(edgeRefs) > 0 {
		edgeStmt, err := tx.PrepareContext(ctx, `INSERT INTO note_edge_links (note_id, edge_source_id, edge_target_id, edge_relation) VALUES (?, ?, ?, ?)`)
		if err != nil {
			return nil, err
		}
		defer edgeStmt.Close()
		for _, ref := range edgeRefs {
			if _, err := edgeStmt.ExecContext(ctx, id, ref.Source, ref.Target, ref.Relation); err != nil {
				return nil, fmt.Errorf("failed to link note to edge %s->%s: %w", ref.Source, ref.Target, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &Note{ID: id, Title: title, Body: body, CreatedAt: createdAt}, nil
}

// GetNote retrieves a note by ID along with its linked node IDs and edge refs.
// Returns nil, nil, nil, nil if not found (matches GetSchema pattern).
func (s *SQLiteStore) GetNote(ctx context.Context, id string) (*Note, []string, []EdgeRef, error) {
	var note Note
	err := s.db.QueryRowContext(ctx, `SELECT id, title, body, created_at FROM notes WHERE id = ?`, id).
		Scan(&note.ID, &note.Title, &note.Body, &note.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil, nil, nil
		}
		return nil, nil, nil, err
	}

	// Fetch linked nodes
	nodeRows, err := s.db.QueryContext(ctx, `SELECT node_id FROM note_node_links WHERE note_id = ?`, id)
	if err != nil {
		return nil, nil, nil, err
	}
	defer nodeRows.Close()

	var nodeIDs []string
	for nodeRows.Next() {
		var nid string
		if err := nodeRows.Scan(&nid); err != nil {
			return nil, nil, nil, err
		}
		nodeIDs = append(nodeIDs, nid)
	}

	// Fetch linked edges
	edgeRows, err := s.db.QueryContext(ctx, `SELECT edge_source_id, edge_target_id, edge_relation FROM note_edge_links WHERE note_id = ?`, id)
	if err != nil {
		return nil, nil, nil, err
	}
	defer edgeRows.Close()

	var edgeRefs []EdgeRef
	for edgeRows.Next() {
		var ref EdgeRef
		if err := edgeRows.Scan(&ref.Source, &ref.Target, &ref.Relation); err != nil {
			return nil, nil, nil, err
		}
		edgeRefs = append(edgeRefs, ref)
	}

	return &note, nodeIDs, edgeRefs, nil
}

// DeleteNote removes a note by ID. CASCADE cleans up link tables.
// Returns an error if the note does not exist.
func (s *SQLiteStore) DeleteNote(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM notes WHERE id = ?`, id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("note '%s' not found", id)
	}
	return nil
}

// deduplicateNoteRefs removes duplicate NoteRef entries by ID.
func deduplicateNoteRefs(refs []NoteRef) []NoteRef {
	seen := make(map[string]struct{}, len(refs))
	out := make([]NoteRef, 0, len(refs))
	for _, r := range refs {
		if _, ok := seen[r.ID]; ok {
			continue
		}
		seen[r.ID] = struct{}{}
		out = append(out, r)
	}
	return out
}
