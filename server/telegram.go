package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const telegramAPIBase = "https://api.telegram.org/bot%s"

type telegramClient struct {
	token      string
	httpClient *http.Client
}

func newTelegramClient(token string) *telegramClient {
	return &telegramClient{
		token: token,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (t *telegramClient) apiURL(method string) string {
	return fmt.Sprintf(telegramAPIBase+"/%s", t.token, method)
}

// SetWebhook registers the plugin's webhook URL with the Telegram Bot API.
func (t *telegramClient) SetWebhook(webhookURL, secretToken string) error {
	payload := map[string]string{
		"url":          webhookURL,
		"secret_token": secretToken,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	resp, err := t.httpClient.Post(t.apiURL("setWebhook"), "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("setWebhook request failed: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("setWebhook decode failed: %w", err)
	}
	if !result.OK {
		return fmt.Errorf("setWebhook error: %s", result.Description)
	}
	return nil
}

// DeleteWebhook removes the registered webhook.
func (t *telegramClient) DeleteWebhook() error {
	resp, err := t.httpClient.Post(t.apiURL("deleteWebhook"), "application/json", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// GetFileURL resolves a file_id to a download URL.
func (t *telegramClient) GetFileURL(fileID string) (string, error) {
	resp, err := t.httpClient.Get(t.apiURL("getFile") + "?file_id=" + fileID)
	if err != nil {
		return "", fmt.Errorf("getFile request failed: %w", err)
	}
	defer resp.Body.Close()

	var result telegramGetFileResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("getFile decode failed: %w", err)
	}
	if !result.OK {
		return "", fmt.Errorf("getFile returned not OK for file_id=%s", fileID)
	}
	if result.Result.FilePath == "" {
		return "", fmt.Errorf("getFile returned empty file_path for file_id=%s", fileID)
	}

	downloadURL := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", t.token, result.Result.FilePath)
	return downloadURL, nil
}

// DownloadFile downloads a file from Telegram's servers.
func (t *telegramClient) DownloadFile(fileURL string) ([]byte, string, error) {
	resp, err := t.httpClient.Get(fileURL)
	if err != nil {
		return nil, "", fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read failed: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")
	return data, contentType, nil
}

// parseUpdate converts a raw Telegram update into a TelegramPost ready for storage.
func parseUpdate(update *telegramUpdate, cetLoc interface{ String() string }) *TelegramPost {
	msg := update.ChannelPost
	if msg == nil {
		return nil
	}

	post := &TelegramPost{
		MessageID:    msg.MessageID,
		DateUTC:      time.Unix(msg.Date, 0).UTC(),
		Caption:      msg.Caption,
		TextContent:  msg.Text,
		MediaGroupID: msg.MediaGroupID,
	}

	// Extract entities (URLs) from message and caption entities
	for _, e := range msg.Entities {
		if e.Type == "url" || e.Type == "text_link" {
			post.Entities = append(post.Entities, TGEntity{Type: e.Type, URL: e.URL})
		}
	}
	for _, e := range msg.CaptionEntities {
		if e.Type == "url" || e.Type == "text_link" {
			post.Entities = append(post.Entities, TGEntity{Type: e.Type, URL: e.URL})
		}
	}

	// Extract bare URLs from text
	if msg.Text != "" {
		for _, e := range msg.Entities {
			if e.Type == "url" && e.Offset+e.Length <= len([]rune(msg.Text)) {
				runes := []rune(msg.Text)
				url := string(runes[e.Offset : e.Offset+e.Length])
				post.Entities = append(post.Entities, TGEntity{Type: "url", URL: url})
			}
		}
	}

	// Determine message type and file_id
	switch {
	case msg.Voice != nil:
		post.MessageType = "voice"
		post.FileID = msg.Voice.FileID
	case msg.Video != nil:
		post.MessageType = "video"
		post.FileID = msg.Video.FileID
	case msg.VideoNote != nil:
		post.MessageType = "video_note"
		post.FileID = msg.VideoNote.FileID
	case len(msg.Photo) > 0:
		post.MessageType = "photo"
		// Use the highest-resolution photo (last in the array)
		post.FileID = msg.Photo[len(msg.Photo)-1].FileID
	default:
		post.MessageType = "text"
	}

	return post
}
