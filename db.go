package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

var migrations = []string{
	`CREATE TABLE IF NOT EXISTS notification (
    notification_id INTEGER PRIMARY KEY ASC,
    title TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    dismissed_at DATETIME DEFAULT NULL,
    remainder_id INTEGER DEFAULT NULL,
    FOREIGN KEY (remainder_id) REFERENCES remainder
);
`,
	`CREATE TABLE IF NOT EXISTS remainder (
    remainder_id INTEGER PRIMARY KEY ASC,
    title TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    scheduled_at DATE NOT NULL,
    period TEXT DEFAULT NULL,
    finished_at DATETIME DEAFULT NULL
);

`,
}

type InvalidDBSchemaErr struct {
	Expected string
	Actual   string
	Index    int
}

func (e InvalidDBSchemaErr) Error() string {
	return "invalid db scheme"
}

type TooNewDBSchemaErr error

type Storage struct {
	db *sql.DB
}

func NewStorage(db *sql.DB) *Storage {
	return &Storage{
		db: db,
	}
}

func explainDBError(err error) string {
	if err == nil {
		return ""
	}

	_, ok := errors.AsType[TooNewDBSchemaErr](err)
	if ok {
		return "Database scheme is too new. Contains more migrations applied than expected. Updated your application\n"

	}

	invalidDBSchema, ok := errors.AsType[InvalidDBSchemaErr](err)
	if ok {
		return strings.Join(
			[]string{fmt.Sprintf("Invalid database scheme. Mismath in migration %d", invalidDBSchema.Index),
				fmt.Sprintf("EXPECTED: %s", invalidDBSchema.Expected),
				fmt.Sprintf("FOUND: %s", invalidDBSchema.Actual)},
			"\n")

	}

	return fmt.Sprintf("%s\n", err.Error())
}

func (s *Storage) CreateSchema(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	sql := `CREATE TABLE IF NOT EXISTS migration (
    applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    query TEXT NOT NULL
);
`
	_, err = tx.ExecContext(ctx, sql, nil)
	if err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	migrationRows, err := tx.QueryContext(ctx, "SELECT query FROM migration")
	if err != nil {
		return fmt.Errorf("failed to get migrations records from db: %w", err)
	}

	var queries []string
	for migrationRows.Next() {
		var query string
		err = migrationRows.Scan(&query)
		if err != nil {
			return err
		}

		queries = append(queries, query)
	}

	closeErr := migrationRows.Close()
	if closeErr != nil {
		return fmt.Errorf("failed to close rows during reading migration table: %w", err)
	}

	if err != nil {
		return fmt.Errorf("failed to scan migration table: %w", err)
	}

	for index, query := range queries {
		if index >= len(migrations) {
			return TooNewDBSchemaErr(errors.New("db scheme too new"))
		}

		if query != migrations[index] {
			err = &InvalidDBSchemaErr{
				Expected: migrations[index],
				Actual:   query,
				Index:    index,
			}

			return err
		}
	}

	for i := len(queries); i < len(migrations); i++ {
		slog.Info("applying migration", "№", i)
		_, err = tx.ExecContext(ctx, migrations[i])
		if err != nil {
			return fmt.Errorf("failed to apply %d migration: %w", i, err)
		}

		_, err = tx.ExecContext(ctx, "INSERT INTO migration (query) VALUES (?)", migrations[i])
		if err != nil {
			return fmt.Errorf("failed to save exequted query into migration table: %w", err)
		}
	}

	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("failed to close transaction: %w", err)
	}

	return nil
}

func (s *Storage) LoadNotificationByID(ctx context.Context, notifID int) (Notification, error) {
	var id int
	var title string
	var (
		createdAtStr string
		dismissedAt  sql.NullTime
	)
	var remainderID sql.NullInt64
	var groupID int

	query := `SELECT
    notification_id,
    title,
    datetime(created_at, 'localtime'),
    datetime(dismissed_at, 'localtime'),
    remainder_id,
    ifnull(remainder_id, -notification_id)
FROM notification WHERE notification_id = ?;`

	rows, err := s.db.QueryContext(ctx, query, notifID)
	if err != nil {
		return Notification{}, fmt.Errorf("failed query notification by id: %w", err)
	}

	for rows.Next() {
		err = rows.Scan(&id, &title, &createdAtStr, &dismissedAt, &remainderID, &groupID)
	}

	closeErr := rows.Close()
	if closeErr != nil {
		return Notification{}, err
	}

	if err != nil {
		return Notification{}, fmt.Errorf("notification row scan failed: %w", err)
	}

	createdAt, err := time.Parse(time.DateTime, createdAtStr)
	if err != nil {
		return Notification{}, fmt.Errorf("invalid created_at value in notification storage %s: %w",
			createdAtStr, err)
	}

	return Notification{
		ID:          id,
		RemainderID: NullInt64(remainderID),
		GroupID:     groupID,
		Title:       title,
		CreatedAt:   createdAt,
		DismissedAt: NullTime(dismissedAt),
	}, nil
}

func (s *Storage) CreateNotificationWithTitle(ctx context.Context, title string) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO notification (title) VALUES (?)", title)
	if err != nil {
		return fmt.Errorf("failed to store notification: %w", err)
	}

	return nil
}

func (s *Storage) GetActiveGroupedNotifications(ctx context.Context) ([]Notification, error) {
	query := `SELECT
    notification_id, title, datetime(created_at, 'localtime') as ts,
     remainder_id, ifnull(remainder_id, -notification_id) as group_id 
FROM notification WHERE dismissed_at IS NULL GROUP BY group_id ORDER BY ts;`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query active notifications: %w", err)
	}

	defer rows.Close()

	result := make([]Notification, 0)
	for rows.Next() {
		var notifID int
		var title string
		var createdAtStr string
		var remainderID sql.NullInt64
		var groupID int

		err = rows.Scan(&notifID, &title, &createdAtStr, &remainderID, &groupID)
		if err != nil {
			return nil, err
		}

		createdAt, err := time.Parse(time.DateTime, createdAtStr)
		if err != nil {
			return nil, fmt.Errorf("invalid created_at in notification %d: %w", notifID, err)
		}

		result = append(result, Notification{
			ID:          notifID,
			RemainderID: NullInt64(remainderID),
			GroupID:     groupID,
			Title:       title,
			CreatedAt:   createdAt,
		})
	}

	if err != nil {
		return nil, err
	}

	return result, nil
}
