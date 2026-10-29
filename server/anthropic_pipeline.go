package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const anthropicMessagesURL = "https://api.anthropic.com/v1/messages"
const anthropicVersion = "2023-06-01"

// maxTokens leaves room for the six-language Liquid of steps E and F plus review notes.
// It stays at the level recommended for non-streaming requests.
const maxTokens = 16000

type anthropicClient struct {
	apiKey     string
	model      string
	endpoint   string
	httpClient *http.Client
}

func newAnthropicClient(apiKey, model string) *anthropicClient {
	return &anthropicClient{
		apiKey:   apiKey,
		model:    model,
		endpoint: anthropicMessagesURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Minute,
		},
	}
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	StopReason string `json:"stop_reason"`
	Content    []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// errAnswerCutOff means the answer hit maxTokens before it was finished.
var errAnswerCutOff = errors.New("answer was cut off at the token limit")

// permanentError marks a failure that sending the same request again cannot fix.
type permanentError struct{ err error }

func (e *permanentError) Error() string { return e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }

// Send sends a single message to the Anthropic API with retry on transient errors.
func (a *anthropicClient) Send(ctx context.Context, system, user string) (string, error) {
	return retryWithBackoff(ctx, 3, func() (string, error) {
		return a.sendOnce(ctx, system, user)
	})
}

func (a *anthropicClient) sendOnce(ctx context.Context, system, user string) (string, error) {
	req := anthropicRequest{
		Model:     a.model,
		MaxTokens: maxTokens,
		System:    system,
		Messages:  []anthropicMessage{{Role: "user", Content: user}},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("x-api-key", a.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)
	httpReq.Header.Set("content-type", "application/json")

	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return "", fmt.Errorf("retryable error status %d: %s", resp.StatusCode, string(respBody))
	}
	if resp.StatusCode >= 400 {
		return "", &permanentError{fmt.Errorf("error status %d: %s", resp.StatusCode, string(respBody))}
	}

	var result anthropicResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if result.Error != nil {
		return "", fmt.Errorf("API error: %s", result.Error.Message)
	}
	if result.StopReason == "max_tokens" {
		return "", &permanentError{fmt.Errorf("%w (%d tokens)", errAnswerCutOff, maxTokens)}
	}
	if result.StopReason == "refusal" {
		return "", &permanentError{errors.New("the model declined to answer (stop reason: refusal)")}
	}

	// Models that think by default put thinking blocks (with empty text) before the
	// answer, so only text blocks make up the answer.
	var text strings.Builder
	for _, block := range result.Content {
		if block.Type == "text" {
			text.WriteString(block.Text)
		}
	}
	if strings.TrimSpace(text.String()) == "" {
		return "", fmt.Errorf("the answer contained no text (stop reason: %s)", result.StopReason)
	}
	return text.String(), nil
}

// retryWithBackoff retries fn up to maxAttempts times with exponential backoff. It stops
// early on a permanentError or when ctx ends.
func retryWithBackoff(ctx context.Context, maxAttempts int, fn func() (string, error)) (string, error) {
	var lastErr error
	delay := 2 * time.Second
	for i := range maxAttempts {
		result, err := fn()
		if err == nil {
			return result, nil
		}
		lastErr = err
		var permanent *permanentError
		if errors.As(err, &permanent) || ctx.Err() != nil {
			break
		}
		if i < maxAttempts-1 {
			if sleepContext(ctx, delay) != nil {
				break
			}
			delay *= 2
		}
	}
	return "", lastErr
}

// sleepContext waits for d, or returns ctx's error if ctx ends first.
func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// pipelineReporter is told when each step starts and is shown each step's output.
type pipelineReporter interface {
	step(name string)
	result(slug, title, text string, liquid bool)
}

// RunPipeline executes all six email pipeline steps and returns the final Liquid output.
func RunPipeline(ctx context.Context, client *anthropicClient, input PipelineInput, report pipelineReporter) (*PipelineResult, error) {
	var warnings []string

	// Step A: Define the CTA
	report.step("Step A: choosing the call to action")
	ctaBrief, err := stepA(ctx, client, input)
	if err != nil {
		return nil, fmt.Errorf("step A (CTA): %w", err)
	}
	report.result("step-a-cta-brief", "Step A: call to action brief", ctaBrief, false)

	// Step B: Write the storyselling email
	report.step("Step B: writing the email")
	emailDraft, err := stepB(ctx, client, input, ctaBrief)
	if err != nil {
		return nil, fmt.Errorf("step B (write email): %w", err)
	}
	report.result("step-b-draft", "Step B: first draft", emailDraft, false)

	// Step C: Review the draft (up to 3 iterations)
	report.step("Step C: reviewing the draft")
	reviewedDraft, reviewWarning, err := stepC(ctx, client, input, emailDraft)
	if err != nil {
		return nil, fmt.Errorf("step C (review): %w", err)
	}
	report.result("step-c-reviewed", "Step C: reviewed draft", reviewedDraft, false)
	if reviewWarning != "" {
		warnings = append(warnings, reviewWarning)
	}

	// Step D: Humanize the copy
	report.step("Step D: humanizing the copy")
	humanizedDraft, err := stepD(ctx, client, input, reviewedDraft)
	if err != nil {
		return nil, fmt.Errorf("step D (humanize): %w", err)
	}
	report.result("step-d-humanized", "Step D: humanized draft", humanizedDraft, false)

	// Steps E and F: one version per language, all at the same time, assembled in code
	report.step("Steps E and F: translating and fluency-checking the 6 languages")
	versions, languageWarnings, err := translateAll(ctx, client, input, humanizedDraft, ctaBrief, report)
	if err != nil {
		return nil, err
	}
	warnings = append(warnings, languageWarnings...)

	title, preHeader, body := assembleEmail(versions)
	if found := findPlaceholders(title + "\n" + preHeader + "\n" + body); len(found) > 0 {
		warnings = append(warnings, "⚠️ Placeholders left in the email, to fix before sending: "+strings.Join(found, ", "))
	}

	return &PipelineResult{
		TitleLiquid:     title,
		PreHeaderLiquid: preHeader,
		BodyLiquid:      body,
		Warnings:        warnings,
	}, nil
}

// bracketRe matches text in square brackets; a following "(" marks a Markdown link.
var bracketRe = regexp.MustCompile(`\[([^\[\]\n]+)\](\(?)`)

// findPlaceholders returns the bracketed texts in s that are not Markdown links, such as
// "[Trait physique à confirmer]", once each.
func findPlaceholders(s string) []string {
	var found []string
	seen := map[string]bool{}
	for _, m := range bracketRe.FindAllStringSubmatch(s, -1) {
		if m[2] == "(" || seen[m[0]] {
			continue
		}
		seen[m[0]] = true
		found = append(found, m[0])
	}
	return found
}

func buildContextSummary(input PipelineInput) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Date: %s\n\n", input.Context.Date))

	if len(input.Context.Transcriptions) > 0 {
		sb.WriteString("## Transcriptions\n\n")
		for _, t := range input.Context.Transcriptions {
			sb.WriteString(fmt.Sprintf("**Message %d (%s)**\n", t.MessageID, t.Type))
			if t.Caption != "" {
				sb.WriteString(fmt.Sprintf("Caption: %s\n", t.Caption))
			}
			sb.WriteString(fmt.Sprintf("Transcription:\n%s\n\n", t.Transcription))
		}
	}

	if len(input.Context.OtherPosts) > 0 {
		sb.WriteString("## Other posts\n\n")
		for _, p := range input.Context.OtherPosts {
			sb.WriteString(fmt.Sprintf("**Message %d (%s)**\n", p.MessageID, p.Type))
			if p.Caption != "" {
				sb.WriteString(fmt.Sprintf("Caption: %s\n", p.Caption))
			}
			if p.Text != "" {
				sb.WriteString(fmt.Sprintf("Text: %s\n", p.Text))
			}
			if p.ImageURL != "" {
				sb.WriteString(fmt.Sprintf("Image URL: %s\n", p.ImageURL))
			}
			if len(p.Links) > 0 {
				sb.WriteString(fmt.Sprintf("Links: %s\n", strings.Join(p.Links, ", ")))
			}
			sb.WriteString("\n")
		}
	}

	if len(input.Context.CTALinksFound) > 0 {
		sb.WriteString("## CTA links found in Telegram posts\n\n")
		for _, u := range input.Context.CTALinksFound {
			sb.WriteString("- " + u + "\n")
		}
	} else {
		sb.WriteString(fmt.Sprintf("## CTA: no links found in posts; use default CTA URL: %s\n", input.DefaultCTAURL))
	}

	return sb.String()
}

func stepA(ctx context.Context, client *anthropicClient, input PipelineInput) (string, error) {
	system := strings.Join([]string{
		"# Brand Profile\n" + input.BrandProfile,
		"# Audience Segment\n" + input.AudienceSegment,
		"# Email Principles\n" + skillEmailPrinciples,
		"# Call to Action Rules\n" + skillCallToAction,
	}, "\n\n---\n\n")

	ctaURL := input.DefaultCTAURL
	if len(input.Context.CTALinksFound) > 0 {
		ctaURL = input.Context.CTALinksFound[0]
	}

	user := fmt.Sprintf(`You are preparing a daily nurture email for %s.

Today's date: %s
Primary CTA URL to use: %s
Default CTA URL (fallback): %s

Links found in today's Telegram posts: %s

Based on the Call to Action rules:
1. Validate the CTA URL destination
2. Define the primary CTA text (primary and alt)
3. Define CTA placement (top, inline, bottom)
4. Output a structured CTA brief

Output only the CTA brief.`,
		input.Context.Date,
		input.Context.Date,
		ctaURL,
		input.DefaultCTAURL,
		func() string {
			if len(input.Context.CTALinksFound) > 0 {
				return strings.Join(input.Context.CTALinksFound, ", ")
			}
			return "none"
		}(),
	)

	return client.Send(ctx, system, user)
}

func stepB(ctx context.Context, client *anthropicClient, input PipelineInput, ctaBrief string) (string, error) {
	system := strings.Join([]string{
		"# Brand Profile\n" + input.BrandProfile,
		"# Audience Segment\n" + input.AudienceSegment,
		"# Avatar\n" + input.Avatar,
		"# Email Principles\n" + skillEmailPrinciples,
		"# Nurture Campaign Rules\n" + skillCampaignNurture,
		"# Storyselling Framework\n" + storysellingFramework,
	}, "\n\n---\n\n")

	user := fmt.Sprintf(`Write a storyselling nurture email for %s using the content below.

## CTA Brief
%s

## Offer Description
%s

## Today's Telegram Content
<telegram_content>
%s
</telegram_content>

Instructions:
- Use the transcription as the raw story material
- Follow the Storyselling Framework exactly: Hook → Setup → Tension → Bridge → CTA
- The Bridge must connect the story's turning point to OneTake using the Offer Description
- Apply Blair Warren persuasion levers where they fit naturally
- Write the subject line using the benefit + curiosity formula
- Include a P.S. if it strengthens the email
- Write in French (the target language for the daily email)
- Use only facts from the transcription. Never write a placeholder, a bracketed note or an instruction to Sébastien (no "[à confirmer]"); if a detail is missing, write the sentence without it
- Use the active voice and name who acts ("OneTake enlève les silences", not "silences enlevés"). Write full sentences with a verb, not nominal phrases ("Tu as une vidéo à tourner et seulement cinq minutes pour le faire", not "Une vidéo à tourner. Cinq minutes.")
- Output: subject line, email body, and P.S. if applicable`,
		input.Context.Date,
		ctaBrief,
		input.OfferDescription,
		buildContextSummary(input),
	)

	return client.Send(ctx, system, user)
}

const (
	stepCMaxIterations = 3
	stepEMaxAttempts   = 2
	stepFMaxIterations = 2
)

func stepC(ctx context.Context, client *anthropicClient, input PipelineInput, draft string) (string, string, error) {
	system := strings.Join([]string{
		"# Brand Profile\n" + input.BrandProfile,
		"# Audience Segment\n" + input.AudienceSegment,
		"# Email Principles\n" + skillEmailPrinciples,
		"# Email Copy Review\n" + skillEmailCopyReview,
	}, "\n\n---\n\n")

	current := draft
	for range stepCMaxIterations {
		user := fmt.Sprintf(`Review the email draft below against all 14 checks, plus these two:
- Placeholders: no bracketed note, placeholder or instruction to the sender may remain. Rewrite the sentence without the missing detail; never invent one.
- Active voice and full sentences: no passive constructions or participle lists ("silences enlevés"), no nominal phrases without a verb.
For every issue found, provide a specific rewrite.

Then output the complete corrected email (subject line, body, and P.S. if any) between <revised_email> and </revised_email> tags. Put only the email inside the tags, with no review notes.
End your response with a final line: VERDICT: PASS or VERDICT: NEEDS REVISION

<email_draft>
%s
</email_draft>`, current)

		result, err := client.Send(ctx, system, user)
		if errors.Is(err, errAnswerCutOff) {
			// A review too long to finish is skipped; the current draft stays.
			continue
		}
		if err != nil {
			return current, "", err
		}

		// Only the corrected email is carried forward; review notes are dropped.
		if revised, ok := extractTagged(result, "revised_email"); ok {
			current = revised
		}

		verdict := finalVerdict(result)
		if strings.Contains(verdict, "PASS") && !strings.Contains(verdict, "NEEDS REVISION") {
			return current, "", nil
		}
	}
	return current, fmt.Sprintf("⚠️ Email copy review loop reached maximum iterations (%d). Proceeding with best available version.", stepCMaxIterations), nil
}

func stepD(ctx context.Context, client *anthropicClient, input PipelineInput, draft string) (string, error) {
	system := strings.Join([]string{
		"# Brand Profile (Voice Section)\n" + input.BrandProfile,
		"# Humanize Copy Rules\n" + skillHumanizeCopy,
	}, "\n\n---\n\n")

	user := fmt.Sprintf(`Review the email below for AI writing artefacts and humanize it.
Remove patterns that mark it as machine-generated. Preserve meaning, structure, and brand voice.
Apply every category, including active voice and full sentences (7) and placeholders (8: none may remain).
Output the full revised copy between <humanized_email> and </humanized_email> tags, with no annotations or commentary inside the tags.

<email>
%s
</email>`, draft)

	result, err := client.Send(ctx, system, user)
	if err != nil {
		return "", err
	}
	if humanized, ok := extractTagged(result, "humanized_email"); ok {
		return humanized, nil
	}
	return strings.TrimSpace(result), nil
}

// extractTagged returns the trimmed text between <tag> and the last </tag> in s.
func extractTagged(s, tag string) (string, bool) {
	open, closing := "<"+tag+">", "</"+tag+">"
	start := strings.Index(s, open)
	if start < 0 {
		return "", false
	}
	start += len(open)
	end := strings.LastIndex(s, closing)
	if end < start {
		return "", false
	}
	content := strings.TrimSpace(s[start:end])
	if content == "" {
		return "", false
	}
	return content, true
}

// finalVerdict returns the upper-cased text following the last "VERDICT:" in s,
// or "" when the model gave no verdict line.
func finalVerdict(s string) string {
	upper := strings.ToUpper(s)
	idx := strings.LastIndex(upper, "VERDICT:")
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(upper[idx+len("VERDICT:"):])
}
