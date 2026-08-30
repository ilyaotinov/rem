package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const remDir = ".rem"
const remDBFile = "db"

type UserError struct {
	Message string
	Err     error
	Usage   *Command
}

func (e *UserError) Error() string {
	return e.Err.Error()
}

func (e *UserError) Unwrap() error {
	return e.Err
}

type RunFn func(cmd *Command, args []string) error

type Command struct {
	Name        string
	Signature   string
	Description string
	Run         RunFn
}

type DescriptionType int

const (
	DescriptionShort DescriptionType = iota
	DescriptionFull
)

func chopByDelim(str string, delim rune) string {
	for i, c := range str {
		if c == delim {
			return str[:i]
		}
	}

	return str
}

func (c *Command) Describe(programName string, pad int, descriptionType DescriptionType) {
	fmt.Printf("%*s%s %s", pad, "", programName, c.Name)
	if c.Signature != "" {
		fmt.Printf(" %s", c.Signature)
	}
	fmt.Println()

	if c.Description != "" {
		switch descriptionType {
		case DescriptionShort:
			shortDescription := chopByDelim(c.Description, '\n')
			fmt.Printf("%*s    %s\n", pad, "", shortDescription)
			if len(strings.Trim(c.Description, "\n")) < len(c.Description) {
				fmt.Printf("%*s    ...\n", pad+2, "")
			}

		case DescriptionFull:
			strList := strings.Split(c.Description, "\n")
			for _, str := range strList {
				fmt.Printf("%*s    %s\n", pad, "", str)
			}
		}
	}
}

var Commands = []Command{
	{
		Name:      "n:new",
		Signature: "<title...>",
		Description: `Add a new Notification manually.
This Notification is not associated with any specific Reminder. You just create
it in the moment to not forget something within the same day.`,
		Run: NotificationNewRun,
	},
	{
		Name:        "n:dismiss",
		Signature:   "<indices...>",
		Description: `Dismiss notifications by specified indices.`,
		Run:         NotificationDismissRun,
	},
	{
		Name:        "n:list",
		Description: "Show list of current Notifications, but unlike `checkout` do not file them off.",
		Run:         NotificationListRun,
	},
	{
		Name:        "r:new",
		Signature:   "<title> <scheduled_at> [period]",
		Description: `Schedule a new reminder`,
		Run:         ReminderNewRun,
	},
	{
		Name:        "r:dismiss",
		Signature:   "<index>",
		Description: "Remove a reminder by index",
		Run:         ReminderDismissRun,
	},
	{
		Name:        "r:list",
		Description: "Show a list of all active Reminders",
		Run:         ReminderListRun,
	},
}

func NotificationNewRun(cmd *Command, args []string) error {
	if len(args) < 1 {
		err := errors.New("expected title")

		return &UserError{
			Message: err.Error(),
			Err:     err,
			Usage:   cmd,
		}
	}

	db, err := OpenRemDB()
	if err != nil {
		return &UserError{
			Message: explainDBError(err),
			Err:     err,
			Usage:   nil,
		}
	}

	defer db.Close()

	ctx := context.TODO()

	tx, err := db.BeginTx(ctx, nil)

	err = CreateNotificationWithTitle(ctx, tx, strings.Join(args, " "))
	if err != nil {
		return err
	}

	err = showActiveNotifications(ctx, tx)
	if err != nil {
		return err
	}
	err = tx.Commit()
	if err != nil {
		return err
	}

	return nil
}

func NotificationDismissRun(cmd *Command, args []string) error {
	if len(args) == 0 {
		return &UserError{
			Message: "expected indices",
			Err:     errors.New("expected indices"),
			Usage:   cmd,
		}
	}

	db, err := OpenRemDB()
	if err != nil {
		return &UserError{
			Message: explainDBError(err),
			Err:     err,
		}
	}

	defer db.Close()

	ctx := context.TODO()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	indices := make([]int, 0, len(args))
	for _, arg := range args {
		val, err := strconv.Atoi(arg)
		if err != nil {
			return &UserError{
				Message: fmt.Sprintf("index must be a number. %s is not", arg),
				Err:     err,
				Usage:   cmd,
			}
		}
		indices = append(indices, val)
	}

	howManyDismissed, err := DismissGroupedNotificationsByIndices(ctx, tx, indices)
	if err != nil {
		return fmt.Errorf("failed to dismiss notifications: %w", err)
	}

	err = showActiveNotifications(ctx, tx)
	if err != nil {
		return err
	}

	err = tx.Commit()
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "Dismissed %d notifications\n", howManyDismissed)

	return nil
}

func NotificationListRun(_ *Command, _ []string) error {
	db, err := OpenRemDB()
	if err != nil {
		return &UserError{
			Message: explainDBError(err),
			Err:     err,
		}
	}

	defer db.Close()

	ctx := context.TODO()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	err = showActiveNotifications(ctx, tx)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func ReminderNewRun(cmd *Command, args []string) error {
	if len(args) < 1 {
		return &UserError{
			Message: "expected title",
			Err:     errors.New("expected title for create reminder"),
			Usage:   cmd,
		}
	}

	if len(args) < 2 {
		return &UserError{
			Message: "expected scheduled_at",
			Err:     errors.New("expected scheduled_at for create reminder"),
			Usage:   cmd,
		}
	}

	title := args[0]
	scheduledAt, err := time.Parse(time.DateOnly, args[1])
	if err != nil {
		return &UserError{
			Message: fmt.Sprintf("scheduled_at must be in format %s", time.DateOnly),
			Err:     err,
		}
	}

	var period Period
	if len(args) >= 3 {
		period, err = ParsePeriodFromStr(args[2])
		if err != nil {
			invalidPeriodErr, ok := errors.AsType[*InvalidPeriodError](err)
			if ok {
				return &UserError{
					Message: invalidPeriodErr.ExplainUsage(),
					Err:     invalidPeriodErr,
				}
			}

			unknownModifierErr, ok := errors.AsType[*UnknownPeriodModifierError](err)
			if ok {
				return &UserError{
					Message: unknownModifierErr.ExplainUsage(),
					Err:     unknownModifierErr,
				}
			}

			return err
		}
	}

	db, err := OpenRemDB()
	if err != nil {
		return &UserError{
			Message: explainDBError(err),
			Err:     err,
		}
	}

	defer db.Close()

	ctx := context.TODO()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	err = CreateNewReminder(ctx, tx, title, scheduledAt, period)
	if err != nil {
		return err
	}

	err = showActiveReminders(ctx, tx)
	if err != nil {
		return err
	}

	err = tx.Commit()
	if err != nil {
		return err
	}

	return nil
}

func ReminderDismissRun(cmd *Command, args []string) error {
	// TODO: add support for multiply indeces like in n:dismisso
	if len(args) <= 0 {
		return &UserError{
			Message: "expected index",
			Err:     errors.New("reminder dismiss expecte index"),
			Usage:   cmd,
		}
	}

	number, err := strconv.Atoi(args[0])
	if err != nil {
		return &UserError{
			Message: fmt.Sprintf("unknown index `%s`", args[0]),
			Err:     err,
		}
	}

	db, err := OpenRemDB()
	if err != nil {
		return &UserError{
			Message: explainDBError(err),
			Err:     err,
		}
	}
	defer db.Close()

	ctx := context.TODO()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	err = RemoveReminderByNumber(ctx, tx, number)
	if err != nil {
		return err
	}

	err = showActiveReminders(ctx, tx)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func ReminderListRun(_ *Command, _ []string) error {
	db, err := OpenRemDB()
	if err != nil {
		return &UserError{
			Message: explainDBError(err),
			Err:     err,
		}
	}
	defer db.Close()

	ctx := context.TODO()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	err = showActiveReminders(ctx, tx)
	if err != nil {
		return err
	}

	err = tx.Commit()
	if err != nil {
		return err
	}

	return nil
}

const DefaultCommand = "n:new"

func main() {
	programName := os.Args[0]
	commandName := DefaultCommand
	if len(os.Args) > 1 {
		commandName = os.Args[1]
	}

	for _, cmd := range Commands {
		if cmd.Name == commandName {
			args := []string{}
			if len(os.Args) >= 2 {
				args = os.Args[2:]
			}
			err := cmd.Run(&cmd, args)
			if err != nil {
				userErr, ok := errors.AsType[*UserError](err)
				if ok {
					if userErr.Usage != nil {
						fmt.Fprintln(os.Stderr, "Usage:")
						userErr.Usage.Describe(programName, 2, DescriptionShort)
					}
					fmt.Fprintf(os.Stderr, "ERROR: %s\n", userErr.Message)

					os.Exit(1)
				}

				fmt.Fprintf(os.Stderr, "ERRRO: %s\n", err.Error())

				os.Exit(1)
			}

			return
		}
	}

	fmt.Printf("ERROR: unknown command `%s`\n", commandName)
	os.Exit(1)
}

func showActiveNotifications(ctx context.Context, tx *sql.Tx) error {
	notificationList, err := GetActiveGroupedNotifications(ctx, tx)
	if err != nil {
		return err
	}

	renderGroupedNotifications(os.Stdout, notificationList)

	return nil
}

func showActiveReminders(ctx context.Context, tx *sql.Tx) error {
	activeReminders, err := GetActiveReminders(ctx, tx)
	if err != nil {
		return err
	}

	for i, reminder := range activeReminders {
		if reminder.Period.Valid {
			fmt.Printf("%d: %s (Scheduled at %s every %s)\n", i, reminder.Title,
				reminder.ScheduledAt.Format(time.DateTime),
				reminder.Period.String)
		} else {
			fmt.Printf("%d: %s (Scheduled at %s)\n", i, reminder.Title,
				reminder.ScheduledAt.Format(time.DateTime))
		}
	}

	return nil
}

func renderGroupedNotifications(w io.Writer, notifications []GroupedNotification) {
	for i, notification := range notifications {
		if notification.GroupCount == 0 {
			panic("notification group count cannot be null")
		}

		if notification.GroupCount == 1 {
			fmt.Fprintf(
				w, "%d: %s (%s)\n",
				i, notification.Title, notification.CreatedAt.Format(time.DateTime),
			)
		} else {
			fmt.Fprintf(w, "%d: [%d] %s (%s)\n",
				i, notification.GroupCount, notification.Title, notification.CreatedAt.Format(time.DateTime))
		}
	}
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

func OpenRemDB() (*sql.DB, error) {
	appDir, err := createRemDirIfNotExists()
	if err != nil {
		return nil, fmt.Errorf("failed to create application directory: %w", err)

	}

	db, err := sql.Open("sqlite3", filepath.Join(appDir, remDBFile))
	if err != nil {
		return nil, fmt.Errorf("failed to open database connection: %w", err)
	}

	runCtx, cancel := context.WithTimeout(context.Background(), time.Second*15)
	defer cancel()

	tx, err := db.BeginTx(runCtx, nil)
	if err != nil {
		return nil, err
	}
	err = CreateSchema(runCtx, tx)
	if err != nil {
		return nil, err
	}

	err = tx.Commit()
	if err != nil {
		return nil, err
	}

	return db, nil
}
