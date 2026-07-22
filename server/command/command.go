package command

import (
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/pluginapi"
)

// Runner is implemented by the plugin to trigger the morning processor.
type Runner interface {
	RunProcessor(date string)
}

// Handler processes /neil slash commands.
type Handler struct {
	client *pluginapi.Client
	runner Runner
}

// Command is the interface for the slash command handler.
type Command interface {
	Handle(args *model.CommandArgs) (*model.CommandResponse, error)
}

const neilCommandTrigger = "neil"

// NewCommandHandler registers the /neil command and returns a handler.
func NewCommandHandler(client *pluginapi.Client, runner Runner) Command {
	err := client.SlashCommand.Register(&model.Command{
		Trigger:          neilCommandTrigger,
		AutoComplete:     true,
		AutoCompleteDesc: "Trigger the daily email processor",
		AutoCompleteHint: "[run] [YYYY-MM-DD]",
		AutocompleteData: model.NewAutocompleteData(neilCommandTrigger, "[run] [YYYY-MM-DD]", "Run the morning processor for a given date"),
	})
	if err != nil {
		client.Log.Error("Failed to register /neil command", "error", err)
	}
	return &Handler{client: client, runner: runner}
}

func (c *Handler) Handle(args *model.CommandArgs) (*model.CommandResponse, error) {
	fields := strings.Fields(args.Command)
	if len(fields) < 2 || strings.ToLower(fields[1]) != "run" {
		return &model.CommandResponse{
			ResponseType: model.CommandResponseTypeEphemeral,
			Text:         "Usage: `/neil run [YYYY-MM-DD]`",
		}, nil
	}

	date := ""
	if len(fields) >= 3 {
		if _, err := time.Parse("2006-01-02", fields[2]); err == nil {
			date = fields[2]
		}
	}

	go c.runner.RunProcessor(date)

	return &model.CommandResponse{
		ResponseType: model.CommandResponseTypeEphemeral,
		Text:         "Neil is running the morning processor. Results will be posted to the configured channel.",
	}, nil
}
