package notify

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"
)

type Notifier struct {
	slackURL   string
	discordURL string
	http       *http.Client
}

func NewNotifier(slackURL, discordURL string) *Notifier {
	return &Notifier{
		slackURL:   slackURL,
		discordURL: discordURL,
		http:       &http.Client{Timeout: 10 * time.Second},
	}
}

func (n *Notifier) Broadcast(message string) error {
	if n.slackURL != "" {
		if err := n.postJSON(n.slackURL, map[string]string{"text": message}); err != nil {
			return err
		}
	}
	if n.discordURL != "" {
		if err := n.postJSON(n.discordURL, map[string]string{"content": message}); err != nil {
			return err
		}
	}
	return nil
}

func (n *Notifier) postJSON(url string, body any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}
