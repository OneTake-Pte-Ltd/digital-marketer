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

	// Step 2.2: Only audio/video posts feed the email; text and photo posts are ignored.
	var mediaPosts []TelegramPost
	for _, post := range posts {
		if isAudioVideo(post.MessageType) {
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

	var transcribed []transcribedPost
	for _, post := range mediaPosts {
		text, err := p.transcribeMediaPost(tgClient, bunnyClient, elevenClient, date, post, cfg.transcriptionLanguage())
		if err != nil {
			p.API.LogError("Neil: media post skipped", "message_id", post.MessageID, "error", err.Error())
			p.postToChannel(fmt.Sprintf("⚠️ Message %d (%s) on %s was skipped: %s. Run `/neil run %s` to retry.", post.MessageID, post.MessageType, date, err.Error(), date))
			continue
		}
		transcribed = append(transcribed, transcribedPost{Post: post, Transcription: text})
	}

	if len(transcribed) == 0 {
		msg := fmt.Sprintf("⚠️ None of the %d audio or video posts for %s could be transcribed. No email generated today. Fix the errors above, then run `/neil run %s`.", len(mediaPosts), date, date)
		p.postToChannel(msg)
		status.Status = "failed_no_transcription"
		status.Error = "no audio or video post could be transcribed"
		status.UpdatedAt = time.Now()
		_ = p.kvstore.StoreRunStatus(date, &status)
		return
	}

	// Step 2.5: Assemble context from the successfully transcribed posts only
	dayCtx := buildDayContext(date, transcribed)

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

// isAudioVideo reports whether a post type carries speech that can be transcribed.
func isAudioVideo(messageType string) bool {
	return messageType == "voice" || messageType == "video" || messageType == "video_note"
}

// transcribedPost is an audio/video post together with its transcription.
type transcribedPost struct {
	Post          TelegramPost
	Transcription string
}

// transcribeMediaPost makes sure the post's media is on the CDN and transcribed, and
// returns the transcription. Work already stored in the media record (from an earlier
// run of the same date) is reused, so a replay only redoes the steps that failed.
func (p *Plugin) transcribeMediaPost(tg *telegramClient, bunny *bunnyCDNClient, el *elevenLabsClient, date string, post TelegramPost, languageCode string) (string, error) {
	var record MediaRecord
	if err := p.kvstore.GetMedia(post.MessageID, &record); err != nil {
		p.API.LogWarn("Neil: failed to read media record, starting fresh", "message_id", post.MessageID, "error", err.Error())
		record = MediaRecord{}
	}

	if strings.TrimSpace(record.Transcription) != "" {
		return record.Transcription, nil
	}

	if record.BunnyCDNURL == "" {
		cdnURL, err := p.uploadMedia(tg, bunny, date, post)
		if err != nil {
			return "", fmt.Errorf("media upload failed: %w", err)
		}
		record.BunnyCDNURL = cdnURL
		if err := p.kvstore.StoreMedia(post.MessageID, &record); err != nil {
			p.API.LogWarn("Neil: failed to store media record", "message_id", post.MessageID, "error", err.Error())
		}
	}

	text, err := el.Transcribe(record.BunnyCDNURL, languageCode)
	if err != nil {
		return "", fmt.Errorf("transcription failed: %w", err)
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("transcription returned no text")
	}

	record.Transcription = text
	if err := p.kvstore.StoreMedia(post.MessageID, &record); err != nil {
		p.API.LogWarn("Neil: failed to store transcription", "message_id", post.MessageID, "error", err.Error())
	}
	return text, nil
}

// uploadMedia fetches the post's file from Telegram and uploads it to BunnyCDN.
func (p *Plugin) uploadMedia(tg *telegramClient, bunny *bunnyCDNClient, date string, post TelegramPost) (string, error) {
	fileURL, err := tg.GetFileURL(post.FileID)
	if err != nil {
		return "", fmt.Errorf("getFileURL: %w", err)
	}

	data, contentType, err := tg.DownloadFile(fileURL)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}

	ext := extensionForContentType(contentType, post.MessageType)
	filename := fmt.Sprintf("%d%s", post.MessageID, ext)

	cdnURL, err := bunny.UploadFile(date, filename, data)
	if err != nil {
		return "", fmt.Errorf("upload: %w", err)
	}
	return cdnURL, nil
}

// buildDayContext assembles the pipeline context from transcribed audio/video posts.
// CTA links are taken only from these posts' captions.
func buildDayContext(date string, transcribed []transcribedPost) DayContext {
	ctx := DayContext{Date: date}
	ctaLinksSet := map[string]bool{}

	for _, tp := range transcribed {
		for _, e := range tp.Post.Entities {
			if e.URL != "" && !ctaLinksSet[e.URL] {
				ctx.CTALinksFound = append(ctx.CTALinksFound, e.URL)
				ctaLinksSet[e.URL] = true
			}
		}
		ctx.Transcriptions = append(ctx.Transcriptions, TranscriptionEntry{
			MessageID:     tp.Post.MessageID,
			Type:          tp.Post.MessageType,
			Transcription: tp.Transcription,
			Caption:       tp.Post.Caption,
		})
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
