package command

import (
	"fmt"
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/pluginapi"
)

// Runner is implemented by the plugin to trigger the morning processor and report on it.
type Runner interface {
	RunProcessor(date string)
	RunStatusReport(date string) string
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

const usage = "Usage: `/neil run [YYYY-MM-DD]` to process a day's posts, or `/neil status [YYYY-MM-DD]` to see where its last run got to. Without a date, Neil uses yesterday."

// NewCommandHandler registers the /neil command and returns a handler.
func NewCommandHandler(client *pluginapi.Client, runner Runner) Command {
	err := client.SlashCommand.Register(commandDefinition())
	if err != nil {
		client.Log.Error("Failed to register /neil command", "error", err)
	}
	return &Handler{client: client, runner: runner}
}

func commandDefinition() *model.Command {
	autocomplete := model.NewAutocompleteData(neilCommandTrigger, "[run|status] [YYYY-MM-DD]", "Run the daily email processor or check on it")
	autocomplete.AddCommand(model.NewAutocompleteData("run", "[YYYY-MM-DD]", "Process a day's Telegram posts (default: yesterday)"))
	autocomplete.AddCommand(model.NewAutocompleteData("status", "[YYYY-MM-DD]", "Show where the last run for a day got to (default: yesterday)"))
	return &model.Command{
		Trigger:          neilCommandTrigger,
		AutoComplete:     true,
		AutoCompleteDesc: "Run the daily email processor or check on it",
		AutoCompleteHint: "[run|status] [YYYY-MM-DD]",
		AutocompleteData: autocomplete,
	}
}

func (c *Handler) Handle(args *model.CommandArgs) (*model.CommandResponse, error) {
	fields := strings.Fields(args.Command)
	if len(fields) < 2 || len(fields) > 3 {
		return ephemeral(usage), nil
	}

	date := ""
	if len(fields) == 3 {
		if _, err := time.Parse("2006-01-02", fields[2]); err != nil {
			return ephemeral(fmt.Sprintf("`%s` is not a date in YYYY-MM-DD format. %s", fields[2], usage)), nil
		}
		date = fields[2]
	}

	switch strings.ToLower(fields[1]) {
	case "run":
		go c.runner.RunProcessor(date)
		day, statusCommand := "yesterday's posts", "/neil status"
		if date != "" {
			day, statusCommand = "the posts from "+date, "/neil status "+date
		}
		return ephemeral(fmt.Sprintf("Neil is processing %s. Progress appears in the configured channel; `%s` shows it too.", day, statusCommand)), nil
	case "status":
		return ephemeral(c.runner.RunStatusReport(date)), nil
	default:
		return ephemeral(usage), nil
	}
}

func ephemeral(text string) *model.CommandResponse {
	return &model.CommandResponse{
		ResponseType: model.CommandResponseTypeEphemeral,
		Text:         text,
	}
}
