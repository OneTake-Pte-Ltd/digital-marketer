package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

const elevenLabsSTTURL = "https://api.elevenlabs.io/v1/speech-to-text"

type elevenLabsClient struct {
	apiKey     string
	httpClient *http.Client
}

func newElevenLabsClient(apiKey string) *elevenLabsClient {
	return &elevenLabsClient{
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 120 * time.Second},
	}
}

// Transcribe sends a public audio/video URL to ElevenLabs Scribe v2 and returns the transcription text.
func (e *elevenLabsClient) Transcribe(sourceURL, languageCode string) (string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	if err := w.WriteField("model_id", "scribe_v2"); err != nil {
		return "", err
	}
	if err := w.WriteField("source_url", sourceURL); err != nil {
		return "", err
	}
	if languageCode != "" {
		if err := w.WriteField("language_code", languageCode); err != nil {
			return "", err
		}
	}
	w.Close()

	req, err := http.NewRequest(http.MethodPost, elevenLabsSTTURL, &buf)
	if err != nil {
		return "", fmt.Errorf("build transcription request: %w", err)
	}
	req.Header.Set("xi-api-key", e.apiKey)
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("transcription request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read transcription response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("transcription failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("decode transcription response: %w", err)
	}
	if result.Text == "" {
		return "", fmt.Errorf("transcription returned empty text")
	}

	return result.Text, nil
}
