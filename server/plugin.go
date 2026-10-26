package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/mattermost/mattermost/server/public/pluginapi"
	"github.com/mattermost/mattermost/server/public/pluginapi/cluster"
	"github.com/pkg/errors"

	"github.com/onetake-pte-ltd/mattermost-plugin-neil/server/command"
	"github.com/onetake-pte-ltd/mattermost-plugin-neil/server/store/kvstore"
)

// Plugin implements the interface expected by the Mattermost server.
type Plugin struct {
	plugin.MattermostPlugin

	kvstore       kvstore.KVStore
	client        *pluginapi.Client
	commandClient command.Command
	router        *mux.Router
	backgroundJob *cluster.Job

	configurationLock sync.RWMutex
	configuration     *configuration

	botUserID string

	// runningDates holds the dates being processed right now, so a date never runs twice at once.
	runningDates sync.Map
}

func (p *Plugin) OnActivate() error {
	p.client = pluginapi.NewClient(p.API, p.Driver)
	p.kvstore = kvstore.NewKVStore(p.client)

	cfg := p.getConfiguration()
	botUserID, err := p.client.Bot.EnsureBot(&model.Bot{
		Username:    "neil",
		DisplayName: cfg.botDisplayName(),
		Description: "Neil; daily storyselling email bot",
	})
	if err != nil {
		return errors.Wrap(err, "failed to ensure bot user")
	}
	p.botUserID = botUserID

	p.commandClient = command.NewCommandHandler(p.client, p)
	p.router = p.initRouter()

	if regErr := p.registerWebhook(); regErr != nil {
		p.API.LogWarn("Neil: failed to register Telegram webhook", "error", regErr.Error())
	}

	job, schedErr := cluster.Schedule(
		p.API,
		"NeilMorningJob",
		cluster.MakeWaitForRoundedInterval(1*time.Hour),
		p.runJob,
	)
	if schedErr != nil {
		return errors.Wrap(schedErr, "failed to schedule background job")
	}
	p.backgroundJob = job

	return nil
}

func (p *Plugin) OnDeactivate() error {
	if p.backgroundJob != nil {
		if err := p.backgroundJob.Close(); err != nil {
			p.API.LogError("Neil: failed to close background job", "error", err)
		}
	}
	return nil
}

func (p *Plugin) ExecuteCommand(c *plugin.Context, args *model.CommandArgs) (*model.CommandResponse, *model.AppError) {
	response, err := p.commandClient.Handle(args)
	if err != nil {
		return nil, model.NewAppError("ExecuteCommand", "plugin.command.execute_command.app_error", nil, err.Error(), http.StatusInternalServerError)
	}
	return response, nil
}

// MessageHasBeenPosted triggers the processor when a user posts "@neil run" in any channel.
func (p *Plugin) MessageHasBeenPosted(c *plugin.Context, post *model.Post) {
	if p.botUserID == "" || post.UserId == p.botUserID {
		return
	}
	msg := strings.ToLower(post.Message)
	if !strings.Contains(msg, "@neil") || !strings.Contains(msg, "run") {
		return
	}
	go p.RunProcessor("")
}

func (p *Plugin) ServeHTTP(c *plugin.Context, w http.ResponseWriter, r *http.Request) {
	p.router.ServeHTTP(w, r)
}

// MattermostAuthorizationRequired is a middleware that requires a valid Mattermost user session.
func (p *Plugin) MattermostAuthorizationRequired(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Mattermost-User-ID") == "" {
			http.Error(w, "Not authorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (p *Plugin) postToChannel(msg string) {
	p.createChannelPost(msg)
}

// createChannelPost posts msg to the configured channel and returns the post, or nil on failure.
func (p *Plugin) createChannelPost(msg string) *model.Post {
	cfg := p.getConfiguration()
	if cfg.ChannelID == "" {
		p.API.LogWarn("Neil: no channel configured, dropping message")
		return nil
	}
	post := &model.Post{
		UserId:    p.botUserID,
		ChannelId: cfg.ChannelID,
		Message:   msg,
	}
	created, appErr := p.API.CreatePost(post)
	if appErr != nil {
		p.API.LogError("Neil: failed to post to channel", "error", appErr.Error())
		return nil
	}
	return created
}

func (p *Plugin) registerWebhook() error {
	if p.kvstore == nil {
		return fmt.Errorf("kvstore not initialized")
	}

	cfg := p.getConfiguration()
	if cfg.TelegramBotToken == "" {
		return nil
	}

	secret, err := p.kvstore.GetWebhookSecret()
	if err != nil || secret == "" {
		b := make([]byte, 32)
		if _, randErr := rand.Read(b); randErr != nil {
			return fmt.Errorf("generate webhook secret: %w", randErr)
		}
		secret = hex.EncodeToString(b)
		if storeErr := p.kvstore.StoreWebhookSecret(secret); storeErr != nil {
			return fmt.Errorf("store webhook secret: %w", storeErr)
		}
	}

	mmCfg := p.API.GetConfig()
	if mmCfg.ServiceSettings.SiteURL == nil || *mmCfg.ServiceSettings.SiteURL == "" {
		return fmt.Errorf("SiteURL not configured")
	}
	webhookURL := fmt.Sprintf("%s/plugins/com.onetake.neil/telegram/webhook", *mmCfg.ServiceSettings.SiteURL)

	tgClient := newTelegramClient(cfg.TelegramBotToken)
	return tgClient.SetWebhook(webhookURL, secret)
}
