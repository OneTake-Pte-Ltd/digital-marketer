package command

import (
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/mattermost/mattermost/server/public/pluginapi"
	"github.com/stretchr/testify/assert"
)

type env struct {
	client *pluginapi.Client
	api    *plugintest.API
}

func setupTest() *env {
	api := &plugintest.API{}
	driver := &plugintest.Driver{}
	client := pluginapi.NewClient(api, driver)
	return &env{client: client, api: api}
}

type mockRunner struct {
	called bool
	date   string
	urls   chan string
}

func (m *mockRunner) RunProcessorURL(sourceURL string) {
	m.urls <- sourceURL
}

func (m *mockRunner) RunProcessor(date string) {
	m.called = true
	m.date = date
}

func (m *mockRunner) RunStatusReport(date string) string {
	return "status for " + date
}

func registerCommandMock(e *env) {
	e.api.On("RegisterCommand", commandDefinition()).Return(nil)
}

func TestNeilRunCommand(t *testing.T) {
	e := setupTest()
	runner := &mockRunner{}
	registerCommandMock(e)

	cmdHandler := NewCommandHandler(e.client, runner)

	response, err := cmdHandler.Handle(&model.CommandArgs{Command: "/neil run"})
	assert.Nil(t, err)
	assert.Equal(t, model.CommandResponseTypeEphemeral, response.ResponseType)
	assert.Contains(t, response.Text, "yesterday")
}

func TestNeilRunCommandWithDate(t *testing.T) {
	e := setupTest()
	runner := &mockRunner{}
	registerCommandMock(e)

	cmdHandler := NewCommandHandler(e.client, runner)

	response, err := cmdHandler.Handle(&model.CommandArgs{Command: "/neil run 2025-01-15"})
	assert.Nil(t, err)
	assert.Equal(t, model.CommandResponseTypeEphemeral, response.ResponseType)
}

func TestNeilUsageHint(t *testing.T) {
	e := setupTest()
	runner := &mockRunner{}
	registerCommandMock(e)

	cmdHandler := NewCommandHandler(e.client, runner)

	response, err := cmdHandler.Handle(&model.CommandArgs{Command: "/neil"})
	assert.Nil(t, err)
	assert.Contains(t, response.Text, "Usage")
}

func TestNeilRunCommandRejectsBadDate(t *testing.T) {
	e := setupTest()
	runner := &mockRunner{}
	registerCommandMock(e)

	cmdHandler := NewCommandHandler(e.client, runner)

	response, err := cmdHandler.Handle(&model.CommandArgs{Command: "/neil run 2026-10-1"})
	assert.Nil(t, err)
	assert.Contains(t, response.Text, "not a date")
	assert.False(t, runner.called, "a mistyped date must not run yesterday instead")
}

func TestNeilStatusCommand(t *testing.T) {
	e := setupTest()
	runner := &mockRunner{}
	registerCommandMock(e)

	cmdHandler := NewCommandHandler(e.client, runner)

	response, err := cmdHandler.Handle(&model.CommandArgs{Command: "/neil status 2026-10-21"})
	assert.Nil(t, err)
	assert.Equal(t, "status for 2026-10-21", response.Text)
	assert.False(t, runner.called)
}

func TestNeilRunCommandWithURL(t *testing.T) {
	e := setupTest()
	runner := &mockRunner{urls: make(chan string, 1)}
	registerCommandMock(e)
	cmdHandler := NewCommandHandler(e.client, runner)

	response, err := cmdHandler.Handle(&model.CommandArgs{Command: "/neil run https://cdn.test/black-friday.mp3"})
	assert.Nil(t, err)
	assert.Contains(t, response.Text, "transcribing")
	select {
	case got := <-runner.urls:
		assert.Equal(t, "https://cdn.test/black-friday.mp3", got)
	case <-time.After(time.Second):
		t.Fatal("RunProcessorURL was not called")
	}

	response, err = cmdHandler.Handle(&model.CommandArgs{Command: "/neil run http://cdn.test/a.mp3"})
	assert.Nil(t, err)
	assert.Contains(t, response.Text, "not an https link")
}
