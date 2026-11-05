package main

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
)

// RunProcessorURL writes an email from the single audio or video file at sourceURL, for
// a post Neil missed on Telegram. ElevenLabs downloads the file itself. The run is named
// after today's date and the file name, so it never blocks or replaces a daily run.
func (p *Plugin) RunProcessorURL(sourceURL string) {
	fileURL := directDownloadURL(sourceURL)
	label := urlRunLabel(time.Now().In(cetLocation), fileURL)
	p.runWith(label, "/neil run "+sourceURL, func(ctx context.Context, status *RunStatus, progress *runProgress) {
		cfg := p.getConfiguration()
		progress.step("Transcribing " + sourceURL)
		text, err := newElevenLabsClient(cfg.ElevenLabsAPIKey).Transcribe(ctx, fileURL, cfg.transcriptionLanguage())
		if err == nil && strings.TrimSpace(text) == "" {
			err = fmt.Errorf("transcription returned no text")
		}
		if err != nil {
			if ctx.Err() != nil {
				p.stopRun(ctx, label, status, progress, "transcription", ctx.Err())
				return
			}
			progress.finish("❌ failed; see the message below")
			p.handleProcessorError(label, status, "transcription", fmt.Errorf("%w. The link must download the file itself, not open a page", err))
			return
		}
		progress.result("transcription", "Transcription of "+sourceURL, text, false)

		post := TelegramPost{MessageType: "audio", DateUTC: time.Now().UTC()}
		p.writeEmail(ctx, label, status, progress, []transcribedPost{{Post: post, Transcription: text}})
	})
}

var driveFileRe = regexp.MustCompile(`^/file/d/([^/]+)`)

// directDownloadURL turns a Dropbox or Google Drive share link, which opens a page, into
// a link that downloads the file. Other links are returned unchanged.
func directDownloadURL(s string) string {
	u, err := url.Parse(s)
	if err != nil {
		return s
	}
	host := strings.ToLower(u.Host)
	switch {
	case host == "www.dropbox.com" || host == "dropbox.com":
		q := u.Query()
		q.Del("dl")
		q.Set("dl", "1")
		u.RawQuery = q.Encode()
		return u.String()
	case host == "drive.google.com":
		id := u.Query().Get("id")
		if m := driveFileRe.FindStringSubmatch(u.Path); m != nil {
			id = m[1]
		}
		if id != "" {
			return "https://drive.google.com/uc?export=download&id=" + url.QueryEscape(id)
		}
	}
	return s
}

var nonSlugRe = regexp.MustCompile(`[^a-z0-9]+`)

// urlRunLabel names a run by URL: the date, then the file name in lowercase letters and
// digits, e.g. "2026-11-05-black-friday". It also names the run's status key and files.
func urlRunLabel(now time.Time, fileURL string) string {
	name := ""
	if u, err := url.Parse(fileURL); err == nil {
		name = path.Base(u.Path)
		name = strings.TrimSuffix(name, path.Ext(name))
	}
	slug := strings.Trim(nonSlugRe.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(slug) > 40 {
		slug = strings.Trim(slug[:40], "-")
	}
	if slug == "" || slug == "uc" {
		slug = "audio"
	}
	return now.Format("2006-01-02") + "-" + slug
}
