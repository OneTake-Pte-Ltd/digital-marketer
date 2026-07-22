package command

import (
	"testing"

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
}

func (m *mockRunner) RunProcessor(date string) {
	m.called = true
	m.date = date
}

func registerCommandMock(e *env) {
	e.api.On("RegisterCommand", &model.Command{
		Trigger:          neilCommandTrigger,
		AutoComplete:     true,
		AutoCompleteDesc: "Trigger the daily email processor",
		AutoCompleteHint: "[run] [YYYY-MM-DD]",
		AutocompleteData: model.NewAutocompleteData(neilCommandTrigger, "[run] [YYYY-MM-DD]", "Run the morning processor for a given date"),
	}).Return(nil)
}

func TestNeilRunCommand(t *testing.T) {
	e := setupTest()
	runner := &mockRunner{}
	registerCommandMock(e)

	cmdHandler := NewCommandHandler(e.client, runner)

	response, err := cmdHandler.Handle(&model.CommandArgs{Command: "/neil run"})
	assert.Nil(t, err)
	assert.Equal(t, model.CommandResponseTypeEphemeral, response.ResponseType)
	assert.Contains(t, response.Text, "processor")
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
