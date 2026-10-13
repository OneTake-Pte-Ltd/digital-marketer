package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const bunnyUploadAttempts = 3

type bunnyCDNClient struct {
	edgeScriptURL    string
	edgeScriptSecret string
	httpClient       *http.Client
	retryDelay       time.Duration
}

func newBunnyCDNClient(edgeScriptURL, secret string) *bunnyCDNClient {
	return &bunnyCDNClient{
		edgeScriptURL:    strings.TrimRight(edgeScriptURL, "/"),
		edgeScriptSecret: secret,
		httpClient:       &http.Client{Timeout: 60 * time.Second},
		retryDelay:       2 * time.Second,
	}
}

// bunnyUploadError is returned by uploadOnce; retryable marks transient failures
// (network errors and 5xx responses) worth another attempt.
type bunnyUploadError struct {
	msg       string
	retryable bool
}

func (e *bunnyUploadError) Error() string { return e.msg }

// UploadFile uploads a file to BunnyCDN via the Edge Script proxy and returns the public CDN URL.
// Files are stored under /neil/{date}/{filename}. Transient failures are retried with backoff.
func (b *bunnyCDNClient) UploadFile(date, filename string, data []byte) (string, error) {
	uploadURL := fmt.Sprintf("%s/upload/neil/%s/%s", b.edgeScriptURL, date, filename)

	delay := b.retryDelay
	var lastErr *bunnyUploadError
	for attempt := 1; attempt <= bunnyUploadAttempts; attempt++ {
		cdnURL, err := b.uploadOnce(uploadURL, data)
		if err == nil {
			return cdnURL, nil
		}
		lastErr = err
		if !err.retryable || attempt == bunnyUploadAttempts {
			break
		}
		time.Sleep(delay)
		delay *= 2
	}
	if lastErr.retryable {
		return "", fmt.Errorf("%s (after %d attempts)", lastErr.msg, bunnyUploadAttempts)
	}
	return "", lastErr
}

func (b *bunnyCDNClient) uploadOnce(uploadURL string, data []byte) (string, *bunnyUploadError) {
	req, err := http.NewRequest(http.MethodPut, uploadURL, bytes.NewReader(data))
	if err != nil {
		return "", &bunnyUploadError{msg: fmt.Sprintf("build upload request: %s", err)}
	}
	req.Header.Set("Authorization", b.edgeScriptSecret)
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return "", &bunnyUploadError{msg: fmt.Sprintf("upload request failed: %s", err), retryable: true}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", &bunnyUploadError{msg: fmt.Sprintf("read upload response: %s", err), retryable: true}
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", &bunnyUploadError{
			msg:       fmt.Sprintf("upload failed with status %d: %s", resp.StatusCode, summarizeErrorBody(body)),
			retryable: resp.StatusCode >= 500,
		}
	}

	var result struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", &bunnyUploadError{msg: fmt.Sprintf("decode upload response: %s", err)}
	}
	if result.URL == "" {
		return "", &bunnyUploadError{msg: "upload response missing url field"}
	}

	return result.URL, nil
}

var (
	htmlScriptStyleRe = regexp.MustCompile(`(?is)<(script|style|head)[^>]*>.*?</(script|style|head)>`)
	htmlTagRe         = regexp.MustCompile(`(?s)<[^>]*>`)
)

const maxErrorBodyLen = 300

// summarizeErrorBody turns an error response body into a short single line.
// HTML error pages (e.g. Bunny's 502 page) are reduced to their visible text.
func summarizeErrorBody(body []byte) string {
	text := string(body)
	if strings.Contains(strings.ToLower(text), "<html") {
		text = htmlScriptStyleRe.ReplaceAllString(text, " ")
		text = htmlTagRe.ReplaceAllString(text, " ")
	}
	text = strings.Join(strings.Fields(text), " ")
	if runes := []rune(text); len(runes) > maxErrorBodyLen {
		text = string(runes[:maxErrorBodyLen]) + "…"
	}
	return text
}
