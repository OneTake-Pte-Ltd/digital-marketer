package main

import "time"

// TelegramPost is a stored Telegram channel post.
type TelegramPost struct {
	MessageID   int64      `json:"message_id"`
	DateUTC     time.Time  `json:"date_utc"`
	MessageType string     `json:"message_type"` // voice, video, video_note, photo, text
	FileID      string     `json:"file_id"`
	Caption     string     `json:"caption"`
	TextContent string     `json:"text_content"`
	Entities    []TGEntity `json:"entities"`
	MediaGroupID string    `json:"media_group_id"`
}

// TGEntity is a URL entity extracted from a Telegram post.
type TGEntity struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// MediaRecord holds the BunnyCDN URL and transcription for a media post.
type MediaRecord struct {
	BunnyCDNURL   string `json:"bunny_cdn_url"`
	Transcription string `json:"transcription"`
}

// RunStatus holds the state of a morning processor run for a given date.
type RunStatus struct {
	Status    string    `json:"status"`         // started, completed, completed_no_content, failed_no_transcription, failed, failed_timeout
	Step      string    `json:"step,omitempty"` // the step the run reached last
	StartedAt time.Time `json:"started_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Error     string    `json:"error,omitempty"`
	Warnings  []string  `json:"warnings,omitempty"`
}

// TranscriptionEntry is a transcribed voice/video post with its metadata.
type TranscriptionEntry struct {
	MessageID     int64  `json:"message_id"`
	Type          string `json:"type"`
	Transcription string `json:"transcription"`
	Caption       string `json:"caption"`
}

// OtherPost is a non-audio/video post (photo, text) from the day's Telegram content.
type OtherPost struct {
	MessageID int64    `json:"message_id"`
	Type      string   `json:"type"`
	Caption   string   `json:"caption,omitempty"`
	Text      string   `json:"text,omitempty"`
	ImageURL  string   `json:"image_url,omitempty"`
	Links     []string `json:"links,omitempty"`
}

// DayContext is the assembled context passed to the email pipeline.
type DayContext struct {
	Date           string               `json:"date"`
	Transcriptions []TranscriptionEntry `json:"transcriptions"`
	OtherPosts     []OtherPost          `json:"other_posts"`
	CTALinksFound  []string             `json:"cta_links_found"`
}

// PipelineInput carries all data needed to run the email generation pipeline.
type PipelineInput struct {
	Context          DayContext
	BrandProfile     string
	AudienceSegment  string
	Avatar           string
	OfferDescription string
	DefaultCTAURL    string
	Model            string
	APIKey           string
}

// PipelineResult is the output of a successful pipeline run.
type PipelineResult struct {
	TitleLiquid     string
	PreHeaderLiquid string
	BodyLiquid      string
	Warnings        []string
}

// telegramUpdate is the raw webhook payload from Telegram.
type telegramUpdate struct {
	UpdateID    int64               `json:"update_id"`
	ChannelPost *telegramMessage    `json:"channel_post"`
}

type telegramMessage struct {
	MessageID    int64             `json:"message_id"`
	Date         int64             `json:"date"`
	Text         string            `json:"text"`
	Caption      string            `json:"caption"`
	Voice        *telegramFile     `json:"voice"`
	Video        *telegramFile     `json:"video"`
	VideoNote    *telegramFile     `json:"video_note"`
	Photo        []telegramFile    `json:"photo"`
	Entities     []telegramEntity  `json:"entities"`
	CaptionEntities []telegramEntity `json:"caption_entities"`
	MediaGroupID string            `json:"media_group_id"`
}

type telegramFile struct {
	FileID   string `json:"file_id"`
	Duration int    `json:"duration"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	FileSize int    `json:"file_size"`
}

type telegramEntity struct {
	Type   string `json:"type"`
	Offset int    `json:"offset"`
	Length int    `json:"length"`
	URL    string `json:"url"`
}

type telegramGetFileResponse struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
	Result struct {
		FileID   string `json:"file_id"`
		FilePath string `json:"file_path"`
		FileSize int    `json:"file_size"`
	} `json:"result"`
}
