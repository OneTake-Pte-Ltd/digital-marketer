package main

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func testVersions() map[string]languageVersion {
	return map[string]languageVersion{
		"fr": {Title: "Titre", PreHeader: "Aperçu", Body: "Bonjour, voici l'histoire."},
		"es": {Title: "Título", PreHeader: "Vista", Body: "Hola, esta es la historia."},
		"":   {Title: "Title", PreHeader: "Preview", Body: "Hello, here is the story."},
	}
}

// reviseReplies revises French and English, and answers garbage for Spanish.
func reviseReplies(prompts *[]string) func(user string) string {
	var mu sync.Mutex
	return func(user string) string {
		mu.Lock()
		*prompts = append(*prompts, user)
		mu.Unlock()
		switch {
		case strings.Contains(user, "Revise the French version"):
			return versionReply("Titre revu", "Bonsoir, voici l'histoire.")
		case strings.Contains(user, "Revise the English version"):
			return versionReply("Revised title", "Good evening, here is the story.")
		default:
			return "no version here"
		}
	}
}

func TestReviseAllKeepsFailedLanguage(t *testing.T) {
	var prompts []string
	client := fakeAnthropicByPrompt(t, reviseReplies(&prompts))

	versions, warnings, revised := reviseAll(t.Context(), client, "brand", testVersions(), "Remplace Bonjour par Bonsoir")

	assert.Equal(t, 2, revised)
	assert.Equal(t, "Bonsoir, voici l'histoire.", versions["fr"].Body)
	assert.Equal(t, "Good evening, here is the story.", versions[""].Body)
	assert.Equal(t, "Hola, esta es la historia.", versions["es"].Body, "a failed language keeps its version")
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "Spanish")

	for _, prompt := range prompts {
		assert.Contains(t, prompt, "Remplace Bonjour par Bonsoir")
		if strings.Contains(prompt, "Revise the English version") {
			assert.Contains(t, prompt, "<french_version>", "other languages see the French version")
		}
		if strings.Contains(prompt, "Revise the French version") {
			assert.NotContains(t, prompt, "<french_version>")
		}
	}
}

func TestReviseEmailPostsInThreadAndKeepsRevision(t *testing.T) {
	p, posts, _ := postRecorder(t)
	api := p.API.(interface {
		On(string, ...interface{}) *mock.Call
	})
	api.On("GetBundlePath").Return("", model.NewAppError("GetBundlePath", "test", nil, "no bundle", 500))
	api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything).Maybe().Return()
	api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe().Return()

	kv := p.kvstore.(*fakeKV)
	require.NoError(t, kv.StoreEmail("root1", &EmailRecord{Label: "2026-11-04", Versions: testVersions()}))

	reply := &model.Post{Id: "reply1", RootId: "root1", ChannelId: "channel", UserId: "seb", Message: "Remplace Bonjour par Bonsoir"}
	record, ok := p.emailThreadRoot(reply)
	require.True(t, ok)

	var prompts []string
	p.reviseEmail(fakeAnthropicByPrompt(t, reviseReplies(&prompts)), reply, record)

	last := (*posts)[len(*posts)-1]
	assert.Equal(t, "root1", last.RootId)
	assert.Contains(t, last.Message, "Revised email; 2026-11-04 (revision 1)")
	assert.Contains(t, last.Message, "Bonsoir, voici l'histoire.")
	assert.Contains(t, last.Message, "Spanish version could not be revised")

	var stored EmailRecord
	require.NoError(t, kv.GetEmail("root1", &stored))
	assert.Equal(t, 1, stored.Revisions)
	assert.Equal(t, "Bonsoir, voici l'histoire.", stored.Versions["fr"].Body, "the next feedback applies to the revision")
}

func TestEmailThreadRootIgnoresOtherThreads(t *testing.T) {
	p, _, _ := postRecorder(t)
	kv := p.kvstore.(*fakeKV)
	require.NoError(t, kv.StoreEmail("root1", &EmailRecord{Label: "2026-11-04", Versions: testVersions()}))

	_, ok := p.emailThreadRoot(&model.Post{RootId: "other", ChannelId: "channel"})
	assert.False(t, ok, "not an email thread")
	_, ok = p.emailThreadRoot(&model.Post{RootId: "root1", ChannelId: "elsewhere"})
	assert.False(t, ok, "another channel")
	_, ok = p.emailThreadRoot(&model.Post{ChannelId: "channel"})
	assert.False(t, ok, "not a reply")
}

func TestDirectDownloadURL(t *testing.T) {
	assert.Equal(t, "https://www.dropbox.com/scl/fi/abc/audio.m4a?dl=1&rlkey=x",
		directDownloadURL("https://www.dropbox.com/scl/fi/abc/audio.m4a?rlkey=x&dl=0"))
	assert.Equal(t, "https://drive.google.com/uc?export=download&id=FILEID",
		directDownloadURL("https://drive.google.com/file/d/FILEID/view?usp=sharing"))
	assert.Equal(t, "https://cdn.test/a.mp3", directDownloadURL("https://cdn.test/a.mp3"))
}

func TestURLRunLabel(t *testing.T) {
	now := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	assert.Equal(t, "2026-11-05-black-friday-2026", urlRunLabel(now, "https://cdn.test/Black%20Friday_2026.mp3"))
	assert.Equal(t, "2026-11-05-audio", urlRunLabel(now, "https://drive.google.com/uc?export=download&id=X"))
}
