package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"shorty/internal/model"

	_ "modernc.org/sqlite"
)

type SQLiteStore struct {
	writeDB *sql.DB
	readDB  *sql.DB
}

func New(dbPath string, migrationsFS embed.FS) (*SQLiteStore, error) {
	pragmas := "_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=cache_size(-64000)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)&_pragma=temp_store(MEMORY)"

	writeDB, err := sql.Open("sqlite", dbPath+"?"+pragmas)
	if err != nil {
		return nil, fmt.Errorf("open write db: %w", err)
	}
	writeDB.SetMaxOpenConns(1)

	readDB, err := sql.Open("sqlite", dbPath+"?mode=ro&"+pragmas)
	if err != nil {
		writeDB.Close()
		return nil, fmt.Errorf("open read db: %w", err)
	}
	readDB.SetMaxOpenConns(4)

	// Verify connections work
	if err := writeDB.Ping(); err != nil {
		writeDB.Close()
		readDB.Close()
		return nil, fmt.Errorf("ping write db: %w", err)
	}

	if err := runMigrations(writeDB, migrationsFS); err != nil {
		writeDB.Close()
		readDB.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return &SQLiteStore{writeDB: writeDB, readDB: readDB}, nil
}

func runMigrations(db *sql.DB, migrationsFS embed.FS) error {
	// Check if schema_version table exists
	var currentVersion int
	var tableName string
	err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='schema_version'").Scan(&tableName)
	if err == sql.ErrNoRows {
		currentVersion = 0
	} else if err != nil {
		return fmt.Errorf("check schema_version table: %w", err)
	} else {
		err = db.QueryRow("SELECT version FROM schema_version LIMIT 1").Scan(&currentVersion)
		if err == sql.ErrNoRows {
			currentVersion = 0
		} else if err != nil {
			return fmt.Errorf("read schema version: %w", err)
		}
	}

	// List migration files
	entries, err := fs.ReadDir(migrationsFS, "sql")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	type migration struct {
		version  int
		filename string
	}

	var migrations []migration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		// Parse version from filename prefix (e.g., "001" from "001_init.sql")
		parts := strings.SplitN(entry.Name(), "_", 2)
		if len(parts) < 2 {
			continue
		}
		v, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		migrations = append(migrations, migration{version: v, filename: entry.Name()})
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].version < migrations[j].version
	})

	for _, m := range migrations {
		if m.version <= currentVersion {
			continue
		}

		content, err := fs.ReadFile(migrationsFS, "sql/"+m.filename)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", m.filename, err)
		}

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin tx for %s: %w", m.filename, err)
		}

		if _, err := tx.Exec(string(content)); err != nil {
			tx.Rollback()
			return fmt.Errorf("execute migration %s: %w", m.filename, err)
		}

		// For the first migration (version was 0), the SQL itself creates schema_version and inserts.
		// For subsequent migrations, update the version.
		if currentVersion > 0 {
			if _, err := tx.Exec("UPDATE schema_version SET version = ?", m.version); err != nil {
				tx.Rollback()
				return fmt.Errorf("update schema_version for %s: %w", m.filename, err)
			}
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", m.filename, err)
		}

		currentVersion = m.version
		slog.Info("applied migration", "file", m.filename, "version", m.version)
	}

	return nil
}

func (s *SQLiteStore) Close() error {
	if err := s.readDB.Close(); err != nil {
		return err
	}
	return s.writeDB.Close()
}

// Stub implementations — will be filled in later phases.

func (s *SQLiteStore) CreateLink(ctx context.Context, code, originalURL string, expiresAt *string) (*model.Link, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *SQLiteStore) GetLinkByCode(ctx context.Context, code string) (*model.Link, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *SQLiteStore) GetLinkByID(ctx context.Context, id int64) (*model.Link, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *SQLiteStore) ListLinks(ctx context.Context, params ListParams) (*ListResult, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *SQLiteStore) UpdateLink(ctx context.Context, id int64, isActive *bool, expiresAt *string) (*model.Link, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *SQLiteStore) DeactivateExpiredLink(ctx context.Context, id int64) error {
	return fmt.Errorf("not implemented")
}

func (s *SQLiteStore) DeleteLink(ctx context.Context, id int64) error {
	return fmt.Errorf("not implemented")
}

func (s *SQLiteStore) CodeExists(ctx context.Context, code string) (bool, error) {
	return false, fmt.Errorf("not implemented")
}

func (s *SQLiteStore) BatchInsertClicks(ctx context.Context, events []ClickEvent) error {
	return fmt.Errorf("not implemented")
}

func (s *SQLiteStore) CreateTag(ctx context.Context, name string) (*model.Tag, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *SQLiteStore) ListTags(ctx context.Context) ([]model.TagWithCount, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *SQLiteStore) TagCount(ctx context.Context) (int, error) {
	return 0, fmt.Errorf("not implemented")
}

func (s *SQLiteStore) GetTagByID(ctx context.Context, id int64) (*model.Tag, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *SQLiteStore) DeleteTag(ctx context.Context, id int64) error {
	return fmt.Errorf("not implemented")
}

func (s *SQLiteStore) SetLinkTags(ctx context.Context, linkID int64, tagNames []string) error {
	return fmt.Errorf("not implemented")
}

func (s *SQLiteStore) GetLinkTags(ctx context.Context, linkID int64) ([]string, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *SQLiteStore) GetLinksTagsBatch(ctx context.Context, linkIDs []int64) (map[int64][]string, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *SQLiteStore) GetClicksByDay(ctx context.Context, linkID int64, since time.Time) ([]model.DayCount, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *SQLiteStore) GetClicksByHour(ctx context.Context, linkID int64, since time.Time) ([]model.HourCount, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *SQLiteStore) GetPeriodClickCount(ctx context.Context, linkID int64, since time.Time) (int, error) {
	return 0, fmt.Errorf("not implemented")
}

func (s *SQLiteStore) GetTotalClickCount(ctx context.Context, linkID int64) (int, error) {
	return 0, fmt.Errorf("not implemented")
}

func (s *SQLiteStore) DeleteClicksOlderThan(ctx context.Context, before time.Time) (int64, error) {
	return 0, fmt.Errorf("not implemented")
}
