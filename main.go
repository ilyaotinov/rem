package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const remDir = ".rem"
const remDBFile = "db"

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

type InvalidDbSchemeErr struct {
	Expected string
	Actual   string
}

func (e InvalidDbSchemeErr) Error() string {
	return "invalid db scheme"
}

var (
	dbSchemeTooNewErr = errors.New("database scheme is too new")
)

func createSchema(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
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
			return dbSchemeTooNewErr
		}

		if query != migrations[index] {
			return &InvalidDbSchemeErr{
				Expected: migrations[index],
				Actual:   query,
			}
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

func createRemDirIfNotExists() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	path := filepath.Join(homeDir, remDir)
	err = os.MkdirAll(path, os.ModePerm)
	if err != nil {
		return "", err
	}

	return path, nil
}

func main() {
	appDir, err := createRemDirIfNotExists()
	if err != nil {
		slog.Error("failed to create application directory", "err", err.Error())

		os.Exit(1)
	}

	db, err := sql.Open("sqlite3", filepath.Join(appDir, remDBFile))
	if err != nil {
		slog.Error("failed to open database connection: ", "err", err.Error())

		return
	}

	defer db.Close()

	runCtx, cancel := context.WithTimeout(context.Background(), time.Second*15)
	defer cancel()

	err = createSchema(runCtx, db)
	if err != nil {
		invalidSchemeErr, ok := errors.AsType[*InvalidDbSchemeErr](err)
		if ok {
			slog.Error("schema is invalid.")
			slog.Error("expected", "value", invalidSchemeErr.Expected)
			slog.Error("actual", "value", invalidSchemeErr.Actual)

			os.Exit(1)
		}

		slog.Error("failed to create schema.", "err", err.Error())

		os.Exit(1)
	}
}
