package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

type RunFn func(cmd *Command, programName string, args []string) error

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
}

func NotificationNewRun(cmd *Command, programName string, args []string) error {
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

	storage := NewStorage(db)

	title := args[0]
	err = storage.CreateNotificationWithTitle(context.TODO(), title)
	if err != nil {
		return err
	}

	notificationList, err := storage.GetActiveGroupedNotifications(context.TODO())
	renderGroupedNotifications(os.Stdout, notificationList)

	return nil
}

func renderGroupedNotifications(w io.Writer, notifications []Notification) {
	panic("implement me")
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

	s := NewStorage(db)

	err = s.CreateSchema(runCtx)
	if err != nil {
		return nil, err
	}

	return db, nil
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
			err := cmd.Run(&cmd, programName, args)
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
