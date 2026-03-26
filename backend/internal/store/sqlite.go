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

// Time parsing helpers for SQLite datetime values.

func parseSQLiteTime(s string) (time.Time, error) {
	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse time: %s", s)
}

func scanNullableTime(ns sql.NullString) *time.Time {
	if !ns.Valid {
		return nil
	}
	t, err := parseSQLiteTime(ns.String)
	if err != nil {
		return nil
	}
	return &t
}

func escapeLike(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "%", "\\%")
	s = strings.ReplaceAll(s, "_", "\\_")
	return s
}

func (s *SQLiteStore) CreateLink(ctx context.Context, code, originalURL string, expiresAt *string) (*model.Link, error) {
	query := `
		INSERT INTO links (code, original_url, expires_at)
		VALUES (?, ?, ?)
		RETURNING id, code, original_url, created_at, expires_at, is_active, click_count, updated_at`

	var link model.Link
	var expiresAtStr sql.NullString
	var isActive int

	err := s.writeDB.QueryRowContext(ctx, query, code, originalURL, expiresAt).Scan(
		&link.ID, &link.Code, &link.OriginalURL,
		&link.CreatedAt, &expiresAtStr, &isActive,
		&link.ClickCount, &link.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert link: %w", err)
	}

	link.IsActive = isActive == 1
	link.ExpiresAt = scanNullableTime(expiresAtStr)

	return &link, nil
}

func (s *SQLiteStore) GetLinkByCode(ctx context.Context, code string) (*model.Link, error) {
	return s.getLinkByFieldFromDB(ctx, s.readDB, "code", code)
}

func (s *SQLiteStore) GetLinkByID(ctx context.Context, id int64) (*model.Link, error) {
	return s.getLinkByFieldFromDB(ctx, s.readDB, "id", id)
}

func (s *SQLiteStore) getLinkByIDFromDB(ctx context.Context, db *sql.DB, id int64) (*model.Link, error) {
	return s.getLinkByFieldFromDB(ctx, db, "id", id)
}

func (s *SQLiteStore) getLinkByFieldFromDB(ctx context.Context, db *sql.DB, field string, value interface{}) (*model.Link, error) {
	query := fmt.Sprintf(
		"SELECT id, code, original_url, created_at, expires_at, is_active, click_count, updated_at FROM links WHERE %s = ?",
		field,
	)

	var link model.Link
	var expiresAt sql.NullString
	var isActive int

	err := db.QueryRowContext(ctx, query, value).Scan(
		&link.ID, &link.Code, &link.OriginalURL,
		&link.CreatedAt, &expiresAt, &isActive,
		&link.ClickCount, &link.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get link by %s: %w", field, err)
	}

	link.IsActive = isActive == 1
	link.ExpiresAt = scanNullableTime(expiresAt)
	return &link, nil
}

func (s *SQLiteStore) ListLinks(ctx context.Context, params ListParams) (*ListResult, error) {
	sortColumn := "created_at"
	switch params.Sort {
	case "created_at", "click_count", "expires_at":
		sortColumn = params.Sort
	case "":
		sortColumn = "created_at"
	default:
		return nil, fmt.Errorf("invalid sort column")
	}

	orderDir := "DESC"
	switch strings.ToLower(params.Order) {
	case "asc":
		orderDir = "ASC"
	case "desc", "":
		orderDir = "DESC"
	default:
		return nil, fmt.Errorf("invalid order direction")
	}

	var conditions []string
	var args []interface{}

	if params.Search != "" {
		conditions = append(conditions, "(original_url LIKE '%' || ? || '%' ESCAPE '\\' OR code LIKE '%' || ? || '%' ESCAPE '\\')")
		escaped := escapeLike(params.Search)
		args = append(args, escaped, escaped)
	}

	if params.Active != nil {
		if *params.Active {
			conditions = append(conditions, "is_active = 1")
		} else {
			conditions = append(conditions, "is_active = 0")
		}
	}

	if params.Tag != "" {
		conditions = append(conditions,
			"id IN (SELECT lt.link_id FROM link_tags lt JOIN tags t ON lt.tag_id = t.id WHERE t.name = ?)")
		args = append(args, params.Tag)
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	countQuery := "SELECT COUNT(*) FROM links " + whereClause
	var total int
	if err := s.readDB.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count links: %w", err)
	}

	offset := (params.Page - 1) * params.PerPage
	dataQuery := fmt.Sprintf(
		"SELECT id, code, original_url, created_at, expires_at, is_active, click_count, updated_at FROM links %s ORDER BY %s %s LIMIT ? OFFSET ?",
		whereClause, sortColumn, orderDir,
	)
	dataArgs := append(args, params.PerPage, offset)

	rows, err := s.readDB.QueryContext(ctx, dataQuery, dataArgs...)
	if err != nil {
		return nil, fmt.Errorf("list links: %w", err)
	}
	defer rows.Close()

	var links []model.Link
	for rows.Next() {
		var link model.Link
		var expiresAt sql.NullString
		var isActive int
		if err := rows.Scan(
			&link.ID, &link.Code, &link.OriginalURL,
			&link.CreatedAt, &expiresAt, &isActive,
			&link.ClickCount, &link.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan link: %w", err)
		}
		link.IsActive = isActive == 1
		link.ExpiresAt = scanNullableTime(expiresAt)
		links = append(links, link)
	}

	return &ListResult{Links: links, Total: total}, nil
}

func (s *SQLiteStore) UpdateLink(ctx context.Context, id int64, isActive *bool, expiresAt *string) (*model.Link, error) {
	var setClauses []string
	var args []interface{}

	if isActive != nil {
		val := 0
		if *isActive {
			val = 1
		}
		setClauses = append(setClauses, "is_active = ?")
		args = append(args, val)
	}

	if expiresAt != nil {
		setClauses = append(setClauses, "expires_at = ?")
		args = append(args, *expiresAt)
	}

	setClauses = append(setClauses, "updated_at = CURRENT_TIMESTAMP")

	query := fmt.Sprintf(
		"UPDATE links SET %s WHERE id = ?",
		strings.Join(setClauses, ", "),
	)
	args = append(args, id)

	result, err := s.writeDB.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("update link: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return nil, ErrNotFound
	}

	return s.getLinkByIDFromDB(ctx, s.writeDB, id)
}

func (s *SQLiteStore) DeactivateExpiredLink(ctx context.Context, id int64) error {
	_, err := s.writeDB.ExecContext(ctx,
		"UPDATE links SET is_active = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ?", id)
	return err
}

func (s *SQLiteStore) DeleteLink(ctx context.Context, id int64) error {
	result, err := s.writeDB.ExecContext(ctx, "DELETE FROM links WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete link: %w", err)
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) CodeExists(ctx context.Context, code string) (bool, error) {
	var exists int
	err := s.readDB.QueryRowContext(ctx,
		"SELECT 1 FROM links WHERE code = ?", code,
	).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
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
