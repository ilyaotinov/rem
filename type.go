package main

import (
	"database/sql"
	"time"
)

type NullInt64 sql.NullInt64
type NullTime sql.NullTime
type NullString sql.NullString

type Notification struct {
	ID          int
	RemainderID NullInt64
	GroupID     int
	Title       string
	CreatedAt   time.Time
	DismissedAt NullTime
}

type GroupedNotification struct {
	Notification
	GroupCount int
}

type Reminder struct {
	ID          int
	Title       string
	ScheduledAt time.Time
	Period      NullString
}

type TgReminder struct {
	ID          string
	Title       string
	ScheduledAt time.Time
}
