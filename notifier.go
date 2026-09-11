package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"time"
)

type Notifier struct {
	Host     string
	Username string
	Password string
}

func (n *Notifier) CreateNewNotifierMessage(ctx context.Context, title string, scheduledAt time.Time) error {
	uuid, err := NewUUIDv4()
	if err != nil {
		return fmt.Errorf("failed to generage uuidv4: %w", err)
	}

	payload := map[string]interface{}{
		"uuid":         uuid,
		"title":        title,
		"scheduled_at": scheduledAt.Format(time.DateTime),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		n.notificationURL(), bytes.NewBuffer(body))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(n.Username, n.Password)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to request notifier: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("unexpected notifier response code: %d", resp.StatusCode)
	}

	return nil
}

func (n *Notifier) GetActiveReminders(ctx context.Context) ([]TgReminder, error) {
	type response struct {
		Records []struct {
			ID          string `json:"id"`
			Title       string `json:"title"`
			ScheduledAt string `json:"scheduled_at"` // time.DateTime format in UTC
		} `json:"records"`
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		n.notificationURL(), nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(n.Username, n.Password)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("unable to get tg reminders from external service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code in get list response: %d", resp.StatusCode)
	}

	respBody := &response{}
	err = json.NewDecoder(resp.Body).Decode(&respBody)
	if err != nil {
		return nil, fmt.Errorf("unexpected response body from external service: %w", err)
	}

	var reminders []TgReminder
	for _, record := range respBody.Records {
		scheduledAt, err := time.Parse(time.DateTime, record.ScheduledAt)
		if err != nil {
			return nil, fmt.Errorf("unexpected time format in get tg reminder list response data: %w",
				err)
		}

		reminders = append(reminders, TgReminder{
			ID:          record.ID,
			Title:       record.Title,
			ScheduledAt: scheduledAt.In(time.Local),
		})
	}

	slices.SortStableFunc(reminders, func(a TgReminder, b TgReminder) int {
		if a.ScheduledAt.After(b.ScheduledAt) {
			return 1
		} else if a.ScheduledAt.Equal(b.ScheduledAt) {
			return 0
		} else {
			return -1
		}
	})

	return reminders, nil
}

func (n *Notifier) DismissReminderByUUID(ctx context.Context, uuid string) error {
	type requestBody struct {
		ID string `json:"id"`
	}

	reqBody := &requestBody{
		ID: uuid,
	}
	reqBodyRaw, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, n.notificationURL(),
		bytes.NewBuffer(reqBodyRaw))
	if err != nil {
		return err
	}

	req.SetBasicAuth(n.Username, n.Password)

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("dissmiss telegram notification request failed: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("unexpected status code from dismiss telegram notification response `%d`",
			response.StatusCode)
	}

	return nil
}

func (n *Notifier) notificationURL() string {
	return fmt.Sprintf("http://%s/notification", n.Host)
}
