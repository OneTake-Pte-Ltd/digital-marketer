package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type bunnyCDNClient struct {
	edgeScriptURL    string
	edgeScriptSecret string
	httpClient       *http.Client
}

func newBunnyCDNClient(edgeScriptURL, secret string) *bunnyCDNClient {
	return &bunnyCDNClient{
		edgeScriptURL:    strings.TrimRight(edgeScriptURL, "/"),
		edgeScriptSecret: secret,
		httpClient:       &http.Client{Timeout: 60 * time.Second},
	}
}

// UploadFile uploads a file to BunnyCDN via the Edge Script proxy and returns the public CDN URL.
// Files are stored under /neil/{date}/{filename}.
func (b *bunnyCDNClient) UploadFile(date, filename string, data []byte) (string, error) {
	uploadURL := fmt.Sprintf("%s/upload/neil/%s/%s", b.edgeScriptURL, date, filename)

	req, err := http.NewRequest(http.MethodPut, uploadURL, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("build upload request: %w", err)
	}
	req.Header.Set("Authorization", b.edgeScriptSecret)
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read upload response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("upload failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("decode upload response: %w", err)
	}
	if result.URL == "" {
		return "", fmt.Errorf("upload response missing url field")
	}

	return result.URL, nil
}
