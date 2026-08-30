package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
)

var migrations = []string{
	`CREATE TABLE IF NOT EXISTS notification (
    notification_id INTEGER PRIMARY KEY ASC,
    title TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    dismissed_at DATETIME DEFAULT NULL,
    reminder_id INTEGER DEFAULT NULL,
    FOREIGN KEY (reminder_id) REFERENCES reminder
);
`,
	`CREATE TABLE IF NOT EXISTS reminder (
    reminder_id INTEGER PRIMARY KEY ASC,
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

func CreateSchema(ctx context.Context, tx *sql.Tx) error {
	sql := `CREATE TABLE IF NOT EXISTS migration (
    applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    query TEXT NOT NULL
);
`
	_, err := tx.ExecContext(ctx, sql, nil)
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

	return nil
}

func LoadNotificationByID(ctx context.Context, tx *sql.Tx, notifID int) (Notification, error) {
	var id int
	var title string
	var (
		createdAtStr string
		dismissedAt  sql.NullTime
	)
	var reminderID sql.NullInt64
	var groupID int

	query := `SELECT
    notification_id,
    title,
    datetime(created_at, 'localtime'),
    datetime(dismissed_at, 'localtime'),
    reminder_id,
    ifnull(reminder_id, -notification_id)
FROM notification WHERE notification_id = ?;`

	rows, err := tx.QueryContext(ctx, query, notifID)
	if err != nil {
		return Notification{}, fmt.Errorf("failed query notification by id: %w", err)
	}

	for rows.Next() {
		err = rows.Scan(&id, &title, &createdAtStr, &dismissedAt, &reminderID, &groupID)
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
		RemainderID: NullInt64(reminderID),
		GroupID:     groupID,
		Title:       title,
		CreatedAt:   createdAt,
		DismissedAt: NullTime(dismissedAt),
	}, nil
}

func CreateNotificationWithTitle(ctx context.Context, tx *sql.Tx, title string) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO notification (title) VALUES (?)", title)
	if err != nil {
		return fmt.Errorf("failed to store notification: %w", err)
	}

	return nil
}

func GetActiveGroupedNotifications(ctx context.Context, tx *sql.Tx) ([]GroupedNotification, error) {
	query := `SELECT
    notification_id, title, datetime(created_at, 'localtime') as ts,
    reminder_id, ifnull(reminder_id, -notification_id) as group_id,
    COUNT(*) as group_count 
FROM notification WHERE dismissed_at IS NULL GROUP BY group_id ORDER BY ts;`

	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query active notifications: %w", err)
	}

	defer rows.Close()

	result := make([]GroupedNotification, 0)
	for rows.Next() {
		var notifID int
		var title string
		var createdAtStr string
		var reminderID sql.NullInt64
		var groupID int
		var groupCount int

		err = rows.Scan(&notifID, &title, &createdAtStr, &reminderID, &groupID, &groupCount)
		if err != nil {
			return nil, err
		}

		createdAt, err := time.Parse(time.DateTime, createdAtStr)
		if err != nil {
			return nil, fmt.Errorf("invalid created_at in notification %d: %w", notifID, err)
		}

		result = append(result, GroupedNotification{
			Notification: Notification{
				ID:          notifID,
				RemainderID: NullInt64(reminderID),
				GroupID:     groupID,
				Title:       title,
				CreatedAt:   createdAt,
			},
			GroupCount: groupCount,
		})
	}

	if err != nil {
		return nil, err
	}

	return result, nil
}

func DismissGroupedNotificationsByIndices(
	ctx context.Context,
	tx *sql.Tx,
	indices []int,
) (int, error) {

	activeNotifications, err := GetActiveGroupedNotifications(ctx, tx)
	if err != nil {
		return 0, err
	}

	var howManyDismissed int
	for _, index := range indices {
		if index < 0 || index >= len(activeNotifications) {
			fmt.Fprintf(os.Stderr,
				"WARNING: %d is not a valid index of an active notification\n", index)

			continue
		}

		err := dismissGroupedNotificationByGroupID(ctx, tx, activeNotifications[index].GroupID)
		if err != nil {
			return 0, err
		}

		howManyDismissed += activeNotifications[index].GroupCount
	}

	return howManyDismissed, nil
}

func CreateNewReminder(
	ctx context.Context, tx *sql.Tx,
	title string, scheduledAt time.Time, period Period) error {
	query := `INSERT INTO reminder (title, scheduled_at, period) VALUES (?, ?, ?)`
	renderedPeriod := period.AsSQLDatetimeModifier()

	_, err := tx.ExecContext(ctx, query, title, scheduledAt, renderedPeriod)
	if err != nil {
		return err
	}

	return nil
}

func GetActiveReminders(ctx context.Context, tx *sql.Tx) ([]Reminder, error) {
	query := `SELECT reminder_id, title, scheduled_at, period FROM
 reminder WHERE finished_at IS NULL ORDER BY scheduled_at DESC`

	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	reminders := make([]Reminder, 0)
	for rows.Next() {
		var id int
		var title string
		var scheduledAt sql.NullTime
		var period sql.NullString
		err = rows.Scan(&id, &title, &scheduledAt, &period)
		if err != nil {
			return nil, err
		}

		reminders = append(reminders, Reminder{
			ID:          id,
			Title:       title,
			ScheduledAt: scheduledAt.Time,
			Period:      NullString(period),
		})
	}

	return reminders, nil
}

func RemoveReminderByNumber(ctx context.Context, tx *sql.Tx, number int) error {
	activeReminders, err := GetActiveReminders(ctx, tx)
	if err != nil {
		return err
	}

	if number < 0 || number >= len(activeReminders) {
		return fmt.Errorf("%d is not a valid index of a reminder", number)
	}

	return removeReminderByID(ctx, tx, activeReminders[number].ID)
}

func FireOffReminders(ctx context.Context, tx *sql.Tx) error {
	// Creating new notifications from fired off reminders
	query := `INSERT INTO notification (title, reminder_id) SELECT title, reminder_id FROM
reminder WHERE scheduled_at <= date('now', 'localtime') AND finished_at IS NULL;`
	_, err := tx.ExecContext(ctx, query)
	if err != nil {
		return err
	}

	// Finish all the non-periodic reminders
	query = `UPDATE reminder SET finished_at = CURRENT_TIMESTAMP WHERE scheduled_at <= date('now', 'localtime') AND finished_at IS NULL AND period IS NULL;`
	_, err = tx.ExecContext(ctx, query)
	if err != nil {
		return err
	}

	// Reschedule all the period reminders
	query = `UPDATE reminder SET scheduled_at = date(scheduled_at, period) WHERE scheduled_at <= date('now', 'localtime') AND finished_at IS NULL AND period IS NOT NULL;`
	_, err = tx.ExecContext(ctx, query)
	if err != nil {
		return err
	}

	return nil
}

func removeReminderByID(ctx context.Context, tx *sql.Tx, id int) error {
	query := "UPDATE reminder SET finished_at = CURRENT_TIMESTAMP WHERE reminder_id = ?"
	_, err := tx.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}

	return nil
}

func dismissGroupedNotificationByGroupID(ctx context.Context, tx *sql.Tx, groupID int) error {
	query := `UPDATE notification SET dismissed_at = CURRENT_TIMESTAMP
    WHERE dismissed_at IS NULL AND ifnull(reminder_id, -notification_id) = ?`

	_, err := tx.ExecContext(ctx, query, groupID)
	if err != nil {
		return err
	}

	return nil
}
