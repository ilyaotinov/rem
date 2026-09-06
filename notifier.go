package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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
		fmt.Sprintf("http://%s/notification/", n.Host), bytes.NewBuffer(body))
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
