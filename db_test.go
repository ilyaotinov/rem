package main

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func TestCreateAndReadNotification(t *testing.T) {
	db, dbPath := newTestDB(t)
	t.Cleanup(func() {
		err := os.RemoveAll(dbPath)
		if err != nil {
			t.Fatalf("failed to remove %s directory: %v", dbPath, err)
		}
	})

	s := NewStorage(db)

	ctx := context.Background()

	err := s.CreateSchema(ctx)
	if err != nil {
		t.Fatalf("failed to create database schema: %v", err)
	}

	title := "new title"

	err = s.CreateNotificationWithTitle(ctx, title)
	if err != nil {
		t.Fatalf("failed to create notification: %v", err)
	}

	notif, err := s.LoadNotificationByID(ctx, 1)
	if err != nil {
		t.Fatalf("failed to load notification by ID: %v", err)
	}

	if notif.Title != title {
		t.Fatalf("unexpected title. expected: %s, got: %s", title, notif.Title)
	}

	if notif.DismissedAt.Valid {
		t.Fatalf("dissmissed_at expected to be null")
	}

	fiveMinutesAgo := time.Now().Add(time.Duration(-5) * time.Minute)
	if notif.CreatedAt.Before(fiveMinutesAgo) {
		t.Fatalf("created_at should be set up to recent datetime, got: %s", notif.CreatedAt.String())
	}

	if notif.GroupID != -1 {
		t.Fatalf("unexpected group_id. expected: %d, got: %d", -1, notif.GroupID)
	}

	if notif.RemainderID.Valid {
		t.Fatalf("remainder_id expected to be null")
	}

	if notif.ID != 1 {
		t.Fatalf("unexpected notification_id. expected: %d, got: %d", 1, notif.ID)
	}
}

func TestStorage_GetActiveGroupedNotifications(t *testing.T) {
	db, path := newTestDB(t)
	t.Cleanup(func() {
		os.RemoveAll(path)
	})

	ctx := context.Background()

	storage := NewStorage(db)
	err := storage.CreateSchema(ctx)
	if err != nil {
		t.Fatalf("failed to prepare scema: %v", err)
	}

	notificationList, err := storage.GetActiveGroupedNotifications(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(notificationList) > 0 {
		t.Fatalf("notification list has unexpected number of elements. expected: %d, got: %d",
			0, len(notificationList))
	}

	err = storage.CreateNotificationWithTitle(ctx, "title 1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	err = storage.CreateNotificationWithTitle(ctx, "title 2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	notificationList, err = storage.GetActiveGroupedNotifications(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(notificationList) != 2 {
		t.Fatalf("notification list has unexpected number of elements. expected: %d, got: %d",
			2, len(notificationList))
	}

}

func newTestDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	path, err := os.MkdirTemp("", "rem-test-dir-*")
	if err != nil {
		t.Fatalf("failed to create temp directory")
	}

	dbPath := filepath.Join(path, "rem_test.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal("failed to open connection too test database")
	}

	return db, dbPath
}
