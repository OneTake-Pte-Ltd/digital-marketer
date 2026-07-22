package main

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
)

func (p *Plugin) initRouter() *mux.Router {
	router := mux.NewRouter()

	// Telegram webhook bypasses Mattermost auth — Telegram sends unauthenticated requests.
	router.HandleFunc("/telegram/webhook", p.handleTelegramWebhook).Methods(http.MethodPost)

	// All other plugin API routes require a valid Mattermost user session.
	api := router.PathPrefix("/api/v1").Subrouter()
	api.Use(p.MattermostAuthorizationRequired)

	return router
}

func (p *Plugin) handleTelegramWebhook(w http.ResponseWriter, r *http.Request) {
	secret, err := p.kvstore.GetWebhookSecret()
	if err != nil || secret == "" {
		http.Error(w, "webhook not configured", http.StatusServiceUnavailable)
		return
	}
	if r.Header.Get("X-Telegram-Bot-Api-Secret-Token") != secret {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var update telegramUpdate
	if decErr := json.NewDecoder(r.Body).Decode(&update); decErr != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	post := parseUpdate(&update, cetLocation)
	if post == nil {
		w.WriteHeader(http.StatusOK)
		return
	}

	date := post.DateUTC.In(cetLocation).Format("2006-01-02")

	if storeErr := p.kvstore.StorePost(date, post.MessageID, post); storeErr != nil {
		p.API.LogError("Neil: failed to store post", "message_id", post.MessageID, "error", storeErr.Error())
	}
	if idxErr := p.kvstore.AppendToIndex(date, post.MessageID); idxErr != nil {
		p.API.LogError("Neil: failed to append to index", "message_id", post.MessageID, "error", idxErr.Error())
	}

	w.WriteHeader(http.StatusOK)
}
