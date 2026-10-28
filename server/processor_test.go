package main

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// fakeKV is an in-memory KVStore. getIndex, when set, replaces GetIndex.
type fakeKV struct {
	mu       sync.Mutex
	statuses map[string][]byte
	media    map[int64]MediaRecord
	getIndex func(date string) ([]int64, error)
}

func (f *fakeKV) StorePost(string, int64, interface{}) error { return nil }
func (f *fakeKV) GetPost(string, int64, interface{}) error   { return nil }
func (f *fakeKV) AppendToIndex(string, int64) error          { return nil }
func (f *fakeKV) StoreMedia(int64, interface{}) error        { return nil }
func (f *fakeKV) GetMedia(id int64, dest interface{}) error {
	if record, ok := f.media[id]; ok {
		*dest.(*MediaRecord) = record
	}
	return nil
}
func (f *fakeKV) StoreWebhookSecret(string) error   { return nil }
func (f *fakeKV) GetWebhookSecret() (string, error) { return "", nil }
func (f *fakeKV) GetIndex(date string) ([]int64, error) {
	if f.getIndex != nil {
		return f.getIndex(date)
	}
	return nil, nil
}

func (f *fakeKV) StoreRunStatus(date string, status interface{}) error {
	data, err := json.Marshal(status)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statuses[date] = data
	return nil
}

func (f *fakeKV) GetRunStatus(date string, dest interface{}) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if data, ok := f.statuses[date]; ok {
		return json.Unmarshal(data, dest)
	}
	return nil
}

func (f *fakeKV) status(t *testing.T, date string) RunStatus {
	t.Helper()
	var s RunStatus
	require.NoError(t, f.GetRunStatus(date, &s))
	return s
}

// testPlugin returns a plugin wired to fake storage and a mock API that records every
// message Neil creates or edits, in order.
func testPlugin(t *testing.T) (*Plugin, *fakeKV, *[]string) {
	t.Helper()
	api := &plugintest.API{}
	var mu sync.Mutex
	var messages []string
	record := func(args mock.Arguments) {
		mu.Lock()
		defer mu.Unlock()
		messages = append(messages, args.Get(0).(*model.Post).Message)
	}
	api.On("CreatePost", mock.Anything).Run(record).Return(func(post *model.Post) *model.Post {
		created := post.Clone()
		created.Id = model.NewId()
		return created
	}, nil)
	api.On("UpdatePost", mock.Anything).Run(record).Return(func(post *model.Post) *model.Post { return post }, nil)
	for _, level := range []string{"LogInfo", "LogWarn", "LogError"} {
		api.On(level, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe().Return()
		api.On(level, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe().Return()
		api.On(level, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe().Return()
		api.On(level, mock.Anything, mock.Anything, mock.Anything).Maybe().Return()
		api.On(level, mock.Anything).Maybe().Return()
	}

	kv := &fakeKV{statuses: map[string][]byte{}}
	p := &Plugin{kvstore: kv, botUserID: "bot"}
	p.SetAPI(api)
	p.setConfiguration(&configuration{ChannelID: "channel"})
	return p, kv, &messages
}

func TestRunProcessorKeepsOneProgressPost(t *testing.T) {
	p, kv, messages := testPlugin(t)

	p.RunProcessor("2026-10-21")

	require.GreaterOrEqual(t, len(*messages), 2)
	first, progress := (*messages)[0], (*messages)[len(*messages)-2]
	assert.Contains(t, first, "**Neil run for 2026-10-21**: ⏳ in progress")
	assert.Contains(t, first, "Run started")
	assert.Contains(t, progress, "nothing to process")
	assert.Contains(t, (*messages)[len(*messages)-1], "No audio or video content")
	assert.Equal(t, "completed_no_content", kv.status(t, "2026-10-21").Status)
}

func TestRunProcessorReportsCrash(t *testing.T) {
	p, kv, messages := testPlugin(t)
	kv.getIndex = func(string) ([]int64, error) { panic("boom") }

	assert.NotPanics(t, func() { p.RunProcessor("2026-10-21") })

	last := (*messages)[len(*messages)-1]
	assert.Contains(t, last, "unexpected crash: boom")
	status := kv.status(t, "2026-10-21")
	assert.Equal(t, "failed", status.Status)
	_, running := p.runningDates.Load("2026-10-21")
	assert.False(t, running, "a crashed run must release its date")
}

func TestRunProcessorRefusesSecondRunForSameDate(t *testing.T) {
	p, kv, messages := testPlugin(t)
	release := make(chan struct{})
	entered := make(chan struct{})
	kv.getIndex = func(string) ([]int64, error) {
		close(entered)
		<-release
		return nil, nil
	}

	done := make(chan struct{})
	go func() {
		p.RunProcessor("2026-10-21")
		close(done)
	}()
	<-entered

	p.RunProcessor("2026-10-21")
	assert.Contains(t, (*messages)[len(*messages)-1], "already processing 2026-10-21")
	assert.Contains(t, p.RunStatusReport("2026-10-21"), "in progress")

	close(release)
	<-done
}

// postRecorder is a mock API that keeps every post Neil creates and every file it uploads.
func postRecorder(t *testing.T) (*Plugin, *[]*model.Post, *[]string) {
	t.Helper()
	api := &plugintest.API{}
	var posts []*model.Post
	var uploads []string
	api.On("CreatePost", mock.Anything).Return(func(post *model.Post) *model.Post {
		created := post.Clone()
		created.Id = model.NewId()
		posts = append(posts, created)
		return created
	}, nil)
	api.On("UpdatePost", mock.Anything).Return(func(post *model.Post) *model.Post { return post }, nil)
	api.On("UploadFile", mock.Anything, "channel", mock.Anything).Return(func(_ []byte, _, name string) *model.FileInfo {
		uploads = append(uploads, name)
		return &model.FileInfo{Id: "file-" + name}
	}, nil)
	api.On("LogInfo", mock.Anything, mock.Anything, mock.Anything).Maybe().Return()

	p := &Plugin{kvstore: &fakeKV{statuses: map[string][]byte{}}, botUserID: "bot"}
	p.SetAPI(api)
	p.setConfiguration(&configuration{ChannelID: "channel"})
	return p, &posts, &uploads
}

func TestPostEmailResultAttachesLongBody(t *testing.T) {
	p, posts, uploads := postRecorder(t)
	body := strings.Repeat("Une longue phrase en six langues. ", 1000)

	require.NoError(t, p.postEmailResult("2026-10-25", &PipelineResult{TitleLiquid: "Titre", PreHeaderLiquid: "Aperçu", BodyLiquid: body}))

	require.Len(t, *posts, 1)
	post := (*posts)[0]
	assert.LessOrEqual(t, utf8.RuneCountInString(post.Message), maxPostRunes)
	assert.Contains(t, post.Message, "Titre")
	assert.Contains(t, post.Message, "neil-2026-10-25-message-body.txt")
	assert.Equal(t, []string{"neil-2026-10-25-message-body.txt"}, *uploads)
	assert.Equal(t, model.StringArray{"file-neil-2026-10-25-message-body.txt"}, post.FileIds)
}

func TestPostEmailResultKeepsShortEmailInline(t *testing.T) {
	p, posts, uploads := postRecorder(t)

	require.NoError(t, p.postEmailResult("2026-10-25", &PipelineResult{TitleLiquid: "Titre", BodyLiquid: "Corps"}))

	require.Len(t, *posts, 1)
	assert.Contains(t, (*posts)[0].Message, "Corps")
	assert.Empty(t, *uploads)
}

func TestStepResultsGoInTheRunThread(t *testing.T) {
	p, posts, uploads := postRecorder(t)
	status := RunStatus{}
	progress := p.newRunProgress("2026-10-25", &status)

	progress.step("Run started")
	progress.result("step-b-draft", "Step B: first draft", "Objet : test", false)
	progress.result("step-e-liquid", "Step E: Liquid", strings.Repeat("x", maxPostRunes), true)

	progress.result("step-c-reviewed", "Step C: reviewed draft", "  ", false)

	require.Len(t, *posts, 4)
	root := (*posts)[0].Id
	assert.Equal(t, root, (*posts)[1].RootId)
	assert.Contains(t, (*posts)[1].Message, "Objet : test")
	assert.Len(t, (*posts)[1].FileIds, 1, "a short result is shown and attached")
	assert.Equal(t, root, (*posts)[2].RootId)
	assert.Contains(t, (*posts)[2].Message, "see the attached file")
	assert.Contains(t, (*posts)[3].Message, "(empty)", "an empty result must be visible as such")
	assert.Equal(t, []string{"neil-2026-10-25-step-b-draft.txt", "neil-2026-10-25-step-e-liquid.txt"}, *uploads)
}

func TestTranscribeMediaPostReusesStoredTranscription(t *testing.T) {
	p, _, _ := postRecorder(t)
	p.kvstore = &fakeKV{media: map[int64]MediaRecord{129: {BunnyCDNURL: "https://cdn.test/129.oga", Transcription: "Bonjour"}}}

	// No clients: any upload or transcription call would panic.
	text, reused, err := p.transcribeMediaPost(context.Background(), nil, nil, nil, "2026-10-25", TelegramPost{MessageID: 129}, "fr")
	require.NoError(t, err)
	assert.True(t, reused)
	assert.Equal(t, "Bonjour", text)
}
