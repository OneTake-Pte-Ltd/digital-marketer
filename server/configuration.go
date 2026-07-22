package main

import (
	"reflect"
	"strings"

	"github.com/pkg/errors"
)

type configuration struct {
	BotDisplayName           string `json:"BotDisplayName"`
	ChannelID                string `json:"ChannelID"`
	UsersToTag               string `json:"UsersToTag"`
	TelegramChannelUsername  string `json:"TelegramChannelUsername"`
	TelegramBotToken         string `json:"TelegramBotToken"`
	BunnyCDNEdgeScriptURL    string `json:"BunnyCDNEdgeScriptURL"`
	BunnyCDNEdgeScriptSecret string `json:"BunnyCDNEdgeScriptSecret"`
	ElevenLabsAPIKey         string `json:"ElevenLabsAPIKey"`
	AnthropicAPIKey          string `json:"AnthropicAPIKey"`
	AnthropicModel           string `json:"AnthropicModel"`
	DefaultCTAURL            string `json:"DefaultCTAURL"`
	OfferDescription         string `json:"OfferDescription"`
	TranscriptionLanguage    string `json:"TranscriptionLanguage"`
}

func (c *configuration) Clone() *configuration {
	clone := *c
	return &clone
}

func (c *configuration) IsValid() error {
	if c.TelegramBotToken == "" {
		return errors.New("Telegram Bot Token is required")
	}
	if c.ChannelID == "" {
		return errors.New("Channel ID is required")
	}
	if c.AnthropicAPIKey == "" {
		return errors.New("Anthropic API Key is required")
	}
	return nil
}

func (c *configuration) botDisplayName() string {
	if c.BotDisplayName == "" {
		return "Neil"
	}
	return c.BotDisplayName
}

func (c *configuration) anthropicModel() string {
	if c.AnthropicModel == "" {
		return "claude-sonnet-4-6"
	}
	return c.AnthropicModel
}

func (c *configuration) transcriptionLanguage() string {
	if c.TranscriptionLanguage == "" {
		return "fr"
	}
	return c.TranscriptionLanguage
}

func (c *configuration) taggedUsernames() []string {
	if c.UsersToTag == "" {
		return nil
	}
	parts := strings.Split(c.UsersToTag, ",")
	result := make([]string, 0, len(parts))
	for _, u := range parts {
		u = strings.TrimSpace(u)
		if u != "" {
			result = append(result, u)
		}
	}
	return result
}

func (p *Plugin) getConfiguration() *configuration {
	p.configurationLock.RLock()
	defer p.configurationLock.RUnlock()

	if p.configuration == nil {
		return &configuration{}
	}

	return p.configuration
}

func (p *Plugin) setConfiguration(configuration *configuration) {
	p.configurationLock.Lock()
	defer p.configurationLock.Unlock()

	if configuration != nil && p.configuration == configuration {
		if reflect.ValueOf(*configuration).NumField() == 0 {
			return
		}
		panic("setConfiguration called with the existing configuration")
	}

	p.configuration = configuration
}

func (p *Plugin) OnConfigurationChange() error {
	configuration := new(configuration)

	if err := p.API.LoadPluginConfiguration(configuration); err != nil {
		return errors.Wrap(err, "failed to load plugin configuration")
	}

	p.setConfiguration(configuration)

	if p.kvstore != nil {
		if regErr := p.registerWebhook(); regErr != nil {
			p.API.LogWarn("Neil: failed to re-register Telegram webhook after config change", "error", regErr.Error())
		}
	}

	return nil
}
