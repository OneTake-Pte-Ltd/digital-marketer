package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"
	"unicode/utf8"
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

// processorTimeout bounds a whole run, so a stuck call cannot keep a run silent for hours.
const processorTimeout = 45 * time.Minute

// RunProcessor executes the full morning pipeline for the given date.
// If date is empty, it uses yesterday's date in CET.
func (p *Plugin) RunProcessor(date string) {
	if date == "" {
		date = yesterdayInCET()
	}

	// One run per date at a time; a second one would post a duplicate email.
	if _, running := p.runningDates.LoadOrStore(date, struct{}{}); running {
		p.postToChannel(fmt.Sprintf("⚠️ Neil is already processing %s. Use `/neil status %s` to follow it.", date, date))
		return
	}
	defer p.runningDates.Delete(date)

	p.API.LogInfo("Neil: starting morning processor", "date", date)

	cfg := p.getConfiguration()

	// Step 2.1: Mark run as started
	status := RunStatus{
		Status:    "started",
		StartedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	progress := p.newRunProgress(date, &status)

	// Without this, a panic would crash the plugin process and end the run without a word.
	defer func() {
		if r := recover(); r != nil {
			p.API.LogError("Neil: processor crashed", "date", date, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			progress.finish("❌ crashed; see the message below")
			p.handleProcessorError(date, &status, status.Step, fmt.Errorf("unexpected crash: %v", r))
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), processorTimeout)
	defer cancel()

	progress.step("Run started")

	// Step 2.1: Load all posts for yesterday
	ids, err := p.kvstore.GetIndex(date)
	if err != nil {
		progress.finish("❌ failed; see the message below")
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
		progress.finish("⚪ nothing to process")
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
		if ctx.Err() != nil {
			break
		}
		progress.step(fmt.Sprintf("Fetching and transcribing message %d (%s)", post.MessageID, post.MessageType))
		text, reused, err := p.transcribeMediaPost(ctx, tgClient, bunnyClient, elevenClient, date, post, cfg.transcriptionLanguage())
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			p.API.LogError("Neil: media post skipped", "message_id", post.MessageID, "error", err.Error())
			p.postToChannel(fmt.Sprintf("⚠️ Message %d (%s) on %s was skipped: %s. Run `/neil run %s` to retry.", post.MessageID, post.MessageType, date, err.Error(), date))
			continue
		}
		title := fmt.Sprintf("Transcription of message %d (%s)", post.MessageID, post.MessageType)
		if reused {
			title += ", reused from an earlier run"
		}
		progress.result(fmt.Sprintf("transcription-%d", post.MessageID), title, text, false)
		transcribed = append(transcribed, transcribedPost{Post: post, Transcription: text})
	}
	if ctx.Err() != nil {
		p.stopRun(ctx, date, &status, progress, "transcription", ctx.Err())
		return
	}
	progress.step(fmt.Sprintf("%d of %d audio or video posts transcribed", len(transcribed), len(mediaPosts)))

	if len(transcribed) == 0 {
		progress.finish("❌ failed; see the message below")
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
		Context:          dayCtx,
		BrandProfile:     brandProfile,
		AudienceSegment:  audienceSegment,
		Avatar:           avatar,
		OfferDescription: cfg.OfferDescription,
		DefaultCTAURL:    cfg.DefaultCTAURL,
		Model:            cfg.anthropicModel(),
		APIKey:           cfg.AnthropicAPIKey,
	}

	result, err := RunPipeline(ctx, anthropicClient, pipelineInput, progress)
	if err != nil {
		p.stopRun(ctx, date, &status, progress, "email pipeline", err)
		return
	}

	// Step 2.7: Post to Mattermost
	if err := p.postEmailResult(date, result); err != nil {
		progress.finish("❌ the email was written but could not be posted; see the message below")
		p.handleProcessorError(date, &status, "post email", err)
		return
	}
	progress.finish("✅ done; the email is posted below")

	status.Status = "completed"
	status.UpdatedAt = time.Now()
	status.Warnings = result.Warnings
	_ = p.kvstore.StoreRunStatus(date, &status)

	p.API.LogInfo("Neil: morning processor completed", "date", date)
}

// stopRun reports an error that ends the run, naming the time limit when that is what stopped it.
func (p *Plugin) stopRun(ctx context.Context, date string, status *RunStatus, progress *runProgress, step string, err error) {
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		progress.finish("❌ failed; see the message below")
		p.handleProcessorError(date, status, step, err)
		return
	}
	minutes := int(processorTimeout.Minutes())
	p.API.LogError("Neil: run timed out", "date", date, "step", status.Step)
	progress.finish(fmt.Sprintf("⏱ stopped after %d minutes; see the message below", minutes))
	p.postToChannel(fmt.Sprintf("⚠️ Neil stopped the run for %s after %d minutes, during \"%s\". Run `/neil run %s` to retry; finished transcriptions are reused.", date, minutes, status.Step, date))
	status.Status = "failed_timeout"
	status.Error = fmt.Sprintf("time limit of %d minutes reached during %q", minutes, status.Step)
	status.UpdatedAt = time.Now()
	_ = p.kvstore.StoreRunStatus(date, status)
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
// run of the same date) is reused, so a replay only redoes the steps that failed; reused
// reports that the transcription itself came from the record.
func (p *Plugin) transcribeMediaPost(ctx context.Context, tg *telegramClient, bunny *bunnyCDNClient, el *elevenLabsClient, date string, post TelegramPost, languageCode string) (text string, reused bool, err error) {
	var record MediaRecord
	if err := p.kvstore.GetMedia(post.MessageID, &record); err != nil {
		p.API.LogWarn("Neil: failed to read media record, starting fresh", "message_id", post.MessageID, "error", err.Error())
		record = MediaRecord{}
	}

	if strings.TrimSpace(record.Transcription) != "" {
		return record.Transcription, true, nil
	}

	if record.BunnyCDNURL == "" {
		cdnURL, err := p.uploadMedia(ctx, tg, bunny, date, post)
		if err != nil {
			return "", false, fmt.Errorf("media upload failed: %w", err)
		}
		record.BunnyCDNURL = cdnURL
		if err := p.kvstore.StoreMedia(post.MessageID, &record); err != nil {
			p.API.LogWarn("Neil: failed to store media record", "message_id", post.MessageID, "error", err.Error())
		}
	}

	text, err = el.Transcribe(ctx, record.BunnyCDNURL, languageCode)
	if err != nil {
		return "", false, fmt.Errorf("transcription failed: %w", err)
	}
	if strings.TrimSpace(text) == "" {
		return "", false, fmt.Errorf("transcription returned no text")
	}

	record.Transcription = text
	if err := p.kvstore.StoreMedia(post.MessageID, &record); err != nil {
		p.API.LogWarn("Neil: failed to store transcription", "message_id", post.MessageID, "error", err.Error())
	}
	return text, false, nil
}

// uploadMedia resolves the post's Telegram download URL and has the Bunny Edge Script
// fetch it directly into BunnyCDN storage.
func (p *Plugin) uploadMedia(ctx context.Context, tg *telegramClient, bunny *bunnyCDNClient, date string, post TelegramPost) (string, error) {
	fileURL, err := tg.GetFileURL(ctx, post.FileID)
	if err != nil {
		return "", fmt.Errorf("getFileURL: %w", err)
	}

	filename := fmt.Sprintf("%d%s", post.MessageID, extensionForFile(fileURL, post.MessageType))

	cdnURL, err := bunny.FetchFile(ctx, date, filename, fileURL)
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

func (p *Plugin) postEmailResult(date string, result *PipelineResult) error {
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

	block := func(heading, liquid string) string {
		if liquid == "" {
			return ""
		}
		return "### " + heading + "\n```liquid\n" + liquid + "\n```\n\n"
	}
	header := fmt.Sprintf("**Daily Email; %s**%s\n\n", date, mentions)
	head := block("Title", result.TitleLiquid) + block("Pre-header", result.PreHeaderLiquid)
	var footer strings.Builder
	for _, w := range result.Warnings {
		footer.WriteString(w + "\n")
	}

	msg := header + head + block("Message body", result.BodyLiquid) + footer.String()
	if utf8.RuneCountInString(msg) <= maxPostRunes {
		_, err := p.createPost("", msg, nil)
		return err
	}

	// The six-language body often exceeds Mattermost's message limit: attach it instead.
	filename := fmt.Sprintf("neil-%s-message-body.txt", date)
	fileID, err := p.attachText(filename, result.BodyLiquid)
	if err != nil {
		return fmt.Errorf("attach the message body: %w", err)
	}
	msg = header + head + "### Message body\n_Too long for one message; it is attached as `" + filename + "`._\n\n" + footer.String()
	_, err = p.createPost("", msg, []string{fileID})
	return err
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

// extensionForFile returns the file extension for a stored media file. Telegram file
// paths carry one (e.g. voice/file_3.oga); the message type is the fallback.
func extensionForFile(fileURL, messageType string) string {
	if ext := strings.ToLower(path.Ext(fileURL)); ext != "" && len(ext) <= 5 {
		return ext
	}
	switch messageType {
	case "voice":
		return ".oga"
	case "video", "video_note":
		return ".mp4"
	default:
		return ".bin"
	}
}
