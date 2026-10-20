package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const bunny502Page = `<!DOCTYPE html>
<html>
<head>
    <title>502 Bad Gateway</title>
    <script src="https://bunny.net/lib/jquery/jquery.min.js"></script>
</head>
<body>
    <div class="alert-details"><h3 class="alert">ERROR 502</h3><h1>Bad Gateway</h1><h3>We could not establish a connection to neil.bunny.run</h3></div>
</body>
</html>`

const testTelegramFileURL = "https://api.telegram.org/file/bot123:SECRET/voice/file_3.oga"

func testBunnyClient(url string) *bunnyCDNClient {
	c := newBunnyCDNClient(url, "secret")
	c.retryDelay = 0
	return c
}

func TestBunnyFetchRetriesTransientErrors(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "secret", r.Header.Get("Authorization"))
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/fetch/neil/2026-10-13/120.oga", r.URL.Path)
		var body struct {
			SourceURL string `json:"source_url"`
		}
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, testTelegramFileURL, body.SourceURL)
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(bunny502Page))
			return
		}
		_, _ = w.Write([]byte(`{"url":"https://cdn.example/neil/2026-10-13/120.oga"}`))
	}))
	defer srv.Close()

	url, err := testBunnyClient(srv.URL).FetchFile("2026-10-13", "120.oga", testTelegramFileURL)
	require.NoError(t, err)
	assert.Equal(t, "https://cdn.example/neil/2026-10-13/120.oga", url)
	assert.Equal(t, int32(3), calls.Load())
}

func TestBunnyFetchGivesUpWithReadableError(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(bunny502Page))
	}))
	defer srv.Close()

	_, err := testBunnyClient(srv.URL).FetchFile("2026-10-13", "120.oga", testTelegramFileURL)
	require.Error(t, err)
	assert.Equal(t, int32(bunnyUploadAttempts), calls.Load())
	assert.Contains(t, err.Error(), "status 502")
	assert.Contains(t, err.Error(), "We could not establish a connection to neil.bunny.run")
	assert.Contains(t, err.Error(), "after 3 attempts")
	assert.NotContains(t, err.Error(), "<")
	assert.NotContains(t, err.Error(), "jquery")
}

func TestBunnyFetchDoesNotRetryClientErrors(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"Unauthorized"}`))
	}))
	defer srv.Close()

	_, err := testBunnyClient(srv.URL).FetchFile("2026-10-13", "120.oga", testTelegramFileURL)
	require.Error(t, err)
	assert.Equal(t, int32(1), calls.Load())
	assert.Contains(t, err.Error(), "Unauthorized")
}

func TestExtensionForFile(t *testing.T) {
	assert.Equal(t, ".oga", extensionForFile(testTelegramFileURL, "voice"))
	assert.Equal(t, ".mp4", extensionForFile("https://api.telegram.org/file/botX/videos/file_5.MP4", "video"))
	assert.Equal(t, ".mp4", extensionForFile("https://api.telegram.org/file/botX/video_notes/file_2", "video_note"))
	assert.Equal(t, ".oga", extensionForFile("https://api.telegram.org/file/botX/voice/file_2", "voice"))
}

func TestSummarizeErrorBodyTruncates(t *testing.T) {
	out := summarizeErrorBody([]byte(strings.Repeat("x", 1000)))
	assert.Equal(t, maxErrorBodyLen+1, len([]rune(out)))
}

func TestTelegramRedactsToken(t *testing.T) {
	tg := newTelegramClient("123:SECRET")
	err := tg.redact(errors.New(`Get "https://api.telegram.org/bot123:SECRET/getFile": timeout`))
	assert.NotContains(t, err.Error(), "SECRET")
}

func TestParseUpdateExtractsCaptionURLs(t *testing.T) {
	// 🎙 is outside the BMP: 2 UTF-16 units, so the URL starts at unit 15, not rune 14.
	caption := "🎙 Écoute ça : https://onetake.ai/demo"
	update := &telegramUpdate{ChannelPost: &telegramMessage{
		MessageID:       120,
		Caption:         caption,
		Voice:           &telegramFile{FileID: "f"},
		CaptionEntities: []telegramEntity{{Type: "url", Offset: 15, Length: 23}},
	}}
	post := parseUpdate(update, cetLocation)
	require.NotNil(t, post)
	var urls []string
	for _, e := range post.Entities {
		if e.URL != "" {
			urls = append(urls, e.URL)
		}
	}
	assert.Equal(t, []string{"https://onetake.ai/demo"}, urls)
}

func TestBuildDayContextUsesOnlyTranscribedPosts(t *testing.T) {
	ctx := buildDayContext("2026-10-13", []transcribedPost{
		{
			Post: TelegramPost{MessageID: 120, MessageType: "voice", Caption: "c",
				Entities: []TGEntity{{URL: "https://a.example"}, {URL: "https://a.example"}}},
			Transcription: "bonjour",
		},
	})
	assert.Len(t, ctx.Transcriptions, 1)
	assert.Equal(t, "bonjour", ctx.Transcriptions[0].Transcription)
	assert.Empty(t, ctx.OtherPosts)
	assert.Equal(t, []string{"https://a.example"}, ctx.CTALinksFound)
}
