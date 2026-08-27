package main

import (
	"database/sql"
	"time"
)

type NullInt64 sql.NullInt64
type NullTime sql.NullTime

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
