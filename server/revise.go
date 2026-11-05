package main

import (
	"context"
	"fmt"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
)

// reviseTimeout bounds one revision of the six languages.
const reviseTimeout = 20 * time.Minute

// reviseMaxAttempts is how many times a language is asked again when its answer has no
// usable version.
const reviseMaxAttempts = 2

// maxFeedbackFileBytes is the largest text file attached to a reply that Neil reads as
// feedback.
const maxFeedbackFileBytes = 200 * 1024

// emailThreadRoot returns the stored email that post replies to, when post is a reply in
// the thread of a final email in the configured channel.
func (p *Plugin) emailThreadRoot(post *model.Post) (*EmailRecord, bool) {
	if post.RootId == "" || post.IsSystemMessage() || post.ChannelId != p.getConfiguration().ChannelID {
		return nil, false
	}
	var record EmailRecord
	if err := p.kvstore.GetEmail(post.RootId, &record); err != nil {
		p.API.LogWarn("Neil: failed to read the email of a thread", "root_id", post.RootId, "error", err.Error())
		return nil, false
	}
	if len(record.Versions) == 0 {
		return nil, false
	}
	return &record, true
}

// ReviseEmail revises the email at the root of reply's thread from the feedback in reply
// (its message and attached text files), then posts the revised email in the thread and
// keeps it as the version the next feedback applies to.
func (p *Plugin) ReviseEmail(reply *model.Post, record *EmailRecord) {
	cfg := p.getConfiguration()
	p.reviseEmail(newAnthropicClient(cfg.AnthropicAPIKey, cfg.anthropicModel()), reply, record)
}

func (p *Plugin) reviseEmail(client *anthropicClient, reply *model.Post, record *EmailRecord) {
	rootID := reply.RootId
	if _, busy := p.revising.LoadOrStore(rootID, struct{}{}); busy {
		p.replyInThread(rootID, "⏳ Neil is still revising this email. Send your feedback again once the revised email is posted.")
		return
	}
	defer p.revising.Delete(rootID)

	feedback := p.feedbackText(reply)
	if strings.TrimSpace(feedback) == "" {
		return
	}

	defer func() {
		if r := recover(); r != nil {
			p.API.LogError("Neil: revision crashed", "root_id", rootID, "panic", fmt.Sprint(r))
			p.replyInThread(rootID, fmt.Sprintf("⚠️ The revision crashed (%v). The email is unchanged; reply again to retry.", r))
		}
	}()

	p.replyInThread(rootID, fmt.Sprintf("✏️ Revising the %d language versions from your feedback…", len(record.Versions)))

	brandProfile, _, _, err := p.loadAssets()
	if err != nil {
		p.API.LogWarn("Neil: failed to load assets for a revision", "error", err.Error())
	}
	ctx, cancel := context.WithTimeout(context.Background(), reviseTimeout)
	defer cancel()

	versions, warnings, revised := reviseAll(ctx, client, brandProfile, record.Versions, feedback)
	if revised == 0 {
		p.replyInThread(rootID, "⚠️ Neil could not revise any language version:\n"+strings.Join(warnings, "\n")+"\nThe email is unchanged; reply again to retry.")
		return
	}

	title, preHeader, body := assembleEmail(versions)
	if found := findPlaceholders(title + "\n" + preHeader + "\n" + body); len(found) > 0 {
		warnings = append(warnings, "⚠️ Placeholders left in the email, to fix before sending: "+strings.Join(found, ", "))
	}
	result := &PipelineResult{TitleLiquid: title, PreHeaderLiquid: preHeader, BodyLiquid: body, Warnings: warnings, Versions: versions}

	record.Revisions++
	header := fmt.Sprintf("**Revised email; %s (revision %d)**\n_Reply in this thread again for another revision._\n\n", record.Label, record.Revisions)
	filename := fmt.Sprintf("neil-%s-revision-%d-message-body.txt", record.Label, record.Revisions)
	if _, err := p.postEmail(rootID, header, filename, result); err != nil {
		p.API.LogError("Neil: failed to post a revised email", "root_id", rootID, "error", err.Error())
		p.replyInThread(rootID, "⚠️ The email was revised but could not be posted: "+err.Error()+". Reply again to retry.")
		return
	}

	record.Versions = versions
	if err := p.kvstore.StoreEmail(rootID, record); err != nil {
		p.API.LogWarn("Neil: failed to store the revised email", "root_id", rootID, "error", err.Error())
		p.replyInThread(rootID, "⚠️ Neil could not save this revision, so the next feedback applies to the previous version.")
	}
}

// replyInThread posts msg in rootID's thread, logging a failure.
func (p *Plugin) replyInThread(rootID, msg string) {
	if _, err := p.createPost(rootID, msg, nil); err != nil {
		p.API.LogError("Neil: failed to reply in a thread", "root_id", rootID, "error", err.Error())
	}
}

// feedbackText is the reply's message followed by the content of its attached text files,
// such as a reviewed copy.
func (p *Plugin) feedbackText(reply *model.Post) string {
	var sb strings.Builder
	sb.WriteString(strings.TrimSpace(reply.Message))
	for _, fileID := range reply.FileIds {
		info, appErr := p.API.GetFileInfo(fileID)
		if appErr != nil {
			p.API.LogWarn("Neil: failed to read a feedback file", "file_id", fileID, "error", appErr.Error())
			continue
		}
		if !isTextFile(info) || info.Size > maxFeedbackFileBytes {
			continue
		}
		data, appErr := p.API.GetFile(fileID)
		if appErr != nil {
			p.API.LogWarn("Neil: failed to read a feedback file", "file_id", fileID, "error", appErr.Error())
			continue
		}
		fmt.Fprintf(&sb, "\n\nAttached file %s:\n%s", info.Name, strings.TrimSpace(string(data)))
	}
	return strings.TrimSpace(sb.String())
}

func isTextFile(info *model.FileInfo) bool {
	if strings.HasPrefix(info.MimeType, "text/") {
		return true
	}
	switch strings.ToLower(path.Ext(info.Name)) {
	case ".txt", ".md", ".liquid", ".html":
		return true
	}
	return false
}

// reviseAll revises every language version at the same time. A language that fails keeps
// its previous version, with a warning. revised counts the languages that were revised.
func reviseAll(ctx context.Context, client *anthropicClient, brandProfile string, current map[string]languageVersion, feedback string) (versions map[string]languageVersion, warnings []string, revised int) {
	type outcome struct {
		version languageVersion
		err     error
	}
	outcomes := make([]outcome, len(emailLanguages))
	french, hasFrench := current["fr"]

	var wg sync.WaitGroup
	for i, lang := range emailLanguages {
		v, ok := current[lang.code]
		if !ok {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			ref := ""
			if lang.code != "fr" && hasFrench {
				ref = french.tagged()
			}
			outcomes[i].version, outcomes[i].err = stepRevise(ctx, client, brandProfile, lang, v, ref, feedback)
		}()
	}
	wg.Wait()

	versions = map[string]languageVersion{}
	for i, lang := range emailLanguages {
		v, ok := current[lang.code]
		if !ok {
			continue
		}
		if err := outcomes[i].err; err != nil {
			warnings = append(warnings, fmt.Sprintf("⚠️ The %s version could not be revised (%s); it is unchanged.", lang.name, err.Error()))
			versions[lang.code] = v
			continue
		}
		versions[lang.code] = outcomes[i].version
		revised++
	}
	return versions, warnings, revised
}

// stepRevise applies the feedback to one language version. frenchVersion, the current
// French version, is given to the other languages so that feedback quoting the French
// text can be matched.
func stepRevise(ctx context.Context, client *anthropicClient, brandProfile string, lang emailLanguage, version languageVersion, frenchVersion, feedback string) (languageVersion, error) {
	system := strings.Join([]string{
		"# Brand Profile\n" + brandProfile,
		"# Language Version Rules\n" + skillEmailLanguageVersion,
	}, "\n\n---\n\n")

	reference := ""
	if frenchVersion != "" {
		reference = fmt.Sprintf(`
The current French version is given so you can find the passages the feedback quotes:
<french_version>
%s
</french_version>
`, frenchVersion)
	}

	user := fmt.Sprintf(`Revise the %s version of the email below according to the sender's feedback.

The feedback can be comments, a list of changes or a full reviewed copy. It is usually written in French, about the French version.
- Apply every change the feedback asks for. When it quotes or rewrites the French text, find the matching passage in this version and make the same change, as a native %s speaker would write it.
- When the feedback is a full reviewed copy, this version must follow it: same content, same order, same links.
- Ignore a comment that only concerns another language.
- Change nothing else: copy every other sentence exactly as it is, in the same order.
- Keep links, UTM parameters, Markdown and Liquid variables exactly as they are, unless the feedback changes them.
- No placeholder or bracketed note. The title and the pre-header are one line each. No signature snippet: Neil adds it.

<feedback>
%s
</feedback>
%s
<version>
%s
</version>

Output the complete revised version exactly like this, with nothing else inside the tags:
<version>
<title>the subject line</title>
<preheader>the pre-header</preheader>
<body>
the message body
</body>
<ps>the P.S., only if the version has one after the revision</ps>
</version>`, lang.name, lang.name, feedback, reference, version.tagged())

	for range reviseMaxAttempts {
		result, err := client.Send(ctx, system, user)
		if err != nil {
			return languageVersion{}, err
		}
		if v, ok := parseLanguageVersion(result); ok {
			return v, nil
		}
	}
	return languageVersion{}, fmt.Errorf("no usable <title> and <body> after %d attempts", reviseMaxAttempts)
}
