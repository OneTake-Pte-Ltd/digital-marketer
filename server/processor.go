package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var cetLocation *time.Location

func init() {
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		// Fallback to UTC+1 fixed offset if timezone data is unavailable
		loc = time.FixedZone("CET", 3600)
	}
	cetLocation = loc
}

// yesterdayInCET returns "YYYY-MM-DD" for yesterday's date in CET.
func yesterdayInCET() string {
	now := time.Now().In(cetLocation)
	yesterday := now.AddDate(0, 0, -1)
	return yesterday.Format("2006-01-02")
}

// currentHourInCET returns the current hour (0-23) in CET.
func currentHourInCET() int {
	return time.Now().In(cetLocation).Hour()
}

// loadAssets reads the three bundled asset files from the plugin bundle path.
func (p *Plugin) loadAssets() (brandProfile, audienceSegment, avatar string, err error) {
	bundlePath, appErr := p.API.GetBundlePath()
	if appErr != nil {
		return "", "", "", fmt.Errorf("GetBundlePath: %s", appErr.Error())
	}

	read := func(name string) (string, error) {
		data, readErr := os.ReadFile(filepath.Join(bundlePath, "assets", name))
		if readErr != nil {
			return "", fmt.Errorf("read %s: %w", name, readErr)
		}
		return string(data), nil
	}

	brandProfile, err = read("brand-profile.md")
	if err != nil {
		return
	}
	audienceSegment, err = read("audience-segment.md")
	if err != nil {
		return
	}
	avatar, err = read("avatar.md")
	return
}

// RunProcessor executes the full morning pipeline for the given date.
// If date is empty, it uses yesterday's date in CET.
func (p *Plugin) RunProcessor(date string) {
	if date == "" {
		date = yesterdayInCET()
	}

	p.API.LogInfo("Neil: starting morning processor", "date", date)

	cfg := p.getConfiguration()

	// Step 2.1: Mark run as started
	status := RunStatus{
		Status:    "started",
		StartedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := p.kvstore.StoreRunStatus(date, &status); err != nil {
		p.API.LogError("Neil: failed to store run status", "error", err.Error())
	}

	// Step 2.1: Load all posts for yesterday
	ids, err := p.kvstore.GetIndex(date)
	if err != nil {
		p.handleProcessorError(date, &status, "load index", err)
		return
	}

	posts := make([]TelegramPost, 0, len(ids))
	for _, id := range ids {
		var post TelegramPost
		if err := p.kvstore.GetPost(date, id, &post); err != nil {
			p.API.LogWarn("Neil: failed to load post", "id", id, "error", err.Error())
			continue
		}
		posts = append(posts, post)
	}

	p.API.LogInfo("Neil: loaded posts", "date", date, "count", len(posts))

	// Step 2.2: Check for audio/video content
	var mediaPosts []TelegramPost
	for _, post := range posts {
		if post.MessageType == "voice" || post.MessageType == "video" || post.MessageType == "video_note" {
			mediaPosts = append(mediaPosts, post)
		}
	}

	if len(mediaPosts) == 0 {
		msg := fmt.Sprintf("⚠️ No audio or video content found on the Telegram channel for %s. No email generated today.", date)
		p.postToChannel(msg)
		status.Status = "completed_no_content"
		status.UpdatedAt = time.Now()
		_ = p.kvstore.StoreRunStatus(date, &status)
		return
	}

	// Step 2.3 & 2.4: Upload media and transcribe
	tgClient := newTelegramClient(cfg.TelegramBotToken)
	bunnyClient := newBunnyCDNClient(cfg.BunnyCDNEdgeScriptURL, cfg.BunnyCDNEdgeScriptSecret)
	elevenClient := newElevenLabsClient(cfg.ElevenLabsAPIKey)

	// Upload all media posts (audio, video, photo)
	var allMediaPosts []TelegramPost
	for _, post := range posts {
		if post.MessageType == "voice" || post.MessageType == "video" || post.MessageType == "video_note" || post.MessageType == "photo" {
			allMediaPosts = append(allMediaPosts, post)
		}
	}

	for _, post := range allMediaPosts {
		if err := p.uploadAndStoreMedia(tgClient, bunnyClient, date, post); err != nil {
			p.API.LogError("Neil: media upload failed", "message_id", post.MessageID, "error", err.Error())
			p.postToChannel(fmt.Sprintf("⚠️ Media upload failed for message %d. Error: %s", post.MessageID, err.Error()))
		}
	}

	// Transcribe voice/video posts
	for _, post := range mediaPosts {
		if err := p.transcribePost(elevenClient, post, cfg.transcriptionLanguage()); err != nil {
			p.API.LogError("Neil: transcription failed", "message_id", post.MessageID, "error", err.Error())
			p.postToChannel(fmt.Sprintf("⚠️ Transcription failed for message %d on %s. Error: %s. Manual intervention required.", post.MessageID, date, err.Error()))
		}
	}

	// Step 2.5: Assemble context
	dayCtx := p.assembleContext(date, posts)

	// Step 2.6: Load static assets
	brandProfile, audienceSegment, avatar, err := p.loadAssets()
	if err != nil {
		p.API.LogWarn("Neil: failed to load assets, using empty strings", "error", err.Error())
	}

	// Run the email pipeline
	anthropicClient := newAnthropicClient(cfg.AnthropicAPIKey, cfg.anthropicModel())

	pipelineInput := PipelineInput{
		Context:         dayCtx,
		BrandProfile:    brandProfile,
		AudienceSegment: audienceSegment,
		Avatar:          avatar,
		OfferDescription: cfg.OfferDescription,
		DefaultCTAURL:   cfg.DefaultCTAURL,
		Model:           cfg.anthropicModel(),
		APIKey:          cfg.AnthropicAPIKey,
	}

	result, err := RunPipeline(anthropicClient, pipelineInput)
	if err != nil {
		p.handleProcessorError(date, &status, "email pipeline", err)
		return
	}

	// Step 2.7: Post to Mattermost
	p.postEmailResult(date, result)

	status.Status = "completed"
	status.UpdatedAt = time.Now()
	status.Warnings = result.Warnings
	_ = p.kvstore.StoreRunStatus(date, &status)

	p.API.LogInfo("Neil: morning processor completed", "date", date)
}

func (p *Plugin) uploadAndStoreMedia(tg *telegramClient, bunny *bunnyCDNClient, date string, post TelegramPost) error {
	fileURL, err := tg.GetFileURL(post.FileID)
	if err != nil {
		return fmt.Errorf("getFileURL: %w", err)
	}

	data, contentType, err := tg.DownloadFile(fileURL)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}

	ext := extensionForContentType(contentType, post.MessageType)
	filename := fmt.Sprintf("%d%s", post.MessageID, ext)

	cdnURL, err := bunny.UploadFile(date, filename, data)
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}

	record := MediaRecord{BunnyCDNURL: cdnURL}
	return p.kvstore.StoreMedia(post.MessageID, &record)
}

func (p *Plugin) transcribePost(el *elevenLabsClient, post TelegramPost, languageCode string) error {
	var record MediaRecord
	if err := p.kvstore.GetMedia(post.MessageID, &record); err != nil || record.BunnyCDNURL == "" {
		return fmt.Errorf("media record not found for message %d", post.MessageID)
	}

	text, err := el.Transcribe(record.BunnyCDNURL, languageCode)
	if err != nil {
		return err
	}

	record.Transcription = text
	return p.kvstore.StoreMedia(post.MessageID, &record)
}

func (p *Plugin) assembleContext(date string, posts []TelegramPost) DayContext {
	ctx := DayContext{Date: date}
	ctaLinksSet := map[string]bool{}

	for _, post := range posts {
		// Collect CTA links from entities
		for _, e := range post.Entities {
			if e.URL != "" && !ctaLinksSet[e.URL] {
				ctx.CTALinksFound = append(ctx.CTALinksFound, e.URL)
				ctaLinksSet[e.URL] = true
			}
		}

		switch post.MessageType {
		case "voice", "video", "video_note":
			var record MediaRecord
			_ = p.kvstore.GetMedia(post.MessageID, &record)
			ctx.Transcriptions = append(ctx.Transcriptions, TranscriptionEntry{
				MessageID:     post.MessageID,
				Type:          post.MessageType,
				Transcription: record.Transcription,
				Caption:       post.Caption,
			})

		case "photo":
			var record MediaRecord
			_ = p.kvstore.GetMedia(post.MessageID, &record)
			op := OtherPost{
				MessageID: post.MessageID,
				Type:      "photo",
				Caption:   post.Caption,
				ImageURL:  record.BunnyCDNURL,
			}
			ctx.OtherPosts = append(ctx.OtherPosts, op)

		case "text":
			var links []string
			for _, e := range post.Entities {
				if e.URL != "" {
					links = append(links, e.URL)
				}
			}
			ctx.OtherPosts = append(ctx.OtherPosts, OtherPost{
				MessageID: post.MessageID,
				Type:      "text",
				Text:      post.TextContent,
				Links:     links,
			})
		}
	}

	return ctx
}

func (p *Plugin) postEmailResult(date string, result *PipelineResult) {
	cfg := p.getConfiguration()

	// Build mention string
	mentions := ""
	if usernames := cfg.taggedUsernames(); len(usernames) > 0 {
		parts := make([]string, len(usernames))
		for i, u := range usernames {
			parts[i] = "@" + u
		}
		mentions = " " + strings.Join(parts, " ")
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**Daily Email; %s**%s\n\n", date, mentions))

	if result.TitleLiquid != "" {
		sb.WriteString("### Title\n")
		sb.WriteString("```liquid\n")
		sb.WriteString(result.TitleLiquid + "\n")
		sb.WriteString("```\n\n")
	}

	if result.PreHeaderLiquid != "" {
		sb.WriteString("### Pre-header\n")
		sb.WriteString("```liquid\n")
		sb.WriteString(result.PreHeaderLiquid + "\n")
		sb.WriteString("```\n\n")
	}

	if result.BodyLiquid != "" {
		sb.WriteString("### Message body\n")
		sb.WriteString("```liquid\n")
		sb.WriteString(result.BodyLiquid + "\n")
		sb.WriteString("```\n\n")
	}

	for _, w := range result.Warnings {
		sb.WriteString(w + "\n")
	}

	p.postToChannel(sb.String())
}

func (p *Plugin) handleProcessorError(date string, status *RunStatus, step string, err error) {
	errMsg := fmt.Sprintf("⚠️ Pipeline failed at step [%s] for %s. Error: %s", step, date, err.Error())
	p.API.LogError("Neil: processor error", "step", step, "date", date, "error", err.Error())
	p.postToChannel(errMsg)
	status.Status = "failed"
	status.Error = err.Error()
	status.UpdatedAt = time.Now()
	_ = p.kvstore.StoreRunStatus(date, status)
}

func extensionForContentType(contentType, messageType string) string {
	switch {
	case strings.Contains(contentType, "ogg"):
		return ".oga"
	case strings.Contains(contentType, "webm"):
		return ".webm"
	case strings.Contains(contentType, "mp4"):
		return ".mp4"
	case strings.Contains(contentType, "jpeg"):
		return ".jpg"
	case strings.Contains(contentType, "png"):
		return ".png"
	case messageType == "voice":
		return ".oga"
	case messageType == "video" || messageType == "video_note":
		return ".mp4"
	case messageType == "photo":
		return ".jpg"
	default:
		return ".bin"
	}
}
