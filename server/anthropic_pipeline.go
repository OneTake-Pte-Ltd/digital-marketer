package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const anthropicMessagesURL = "https://api.anthropic.com/v1/messages"
const anthropicVersion = "2023-06-01"
const maxTokens = 8192

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
			Timeout: 300 * time.Second,
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
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Send sends a single message to the Anthropic API with retry on transient errors.
func (a *anthropicClient) Send(system, user string) (string, error) {
	return retryWithBackoff(3, func() (string, error) {
		return a.sendOnce(system, user)
	})
}

func (a *anthropicClient) sendOnce(system, user string) (string, error) {
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

	httpReq, err := http.NewRequest(http.MethodPost, a.endpoint, bytes.NewReader(body))
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

	var result anthropicResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if result.Error != nil {
		return "", fmt.Errorf("API error: %s", result.Error.Message)
	}
	if len(result.Content) == 0 {
		return "", fmt.Errorf("empty response from API")
	}

	return result.Content[0].Text, nil
}

// retryWithBackoff retries fn up to maxAttempts times with exponential backoff.
func retryWithBackoff(maxAttempts int, fn func() (string, error)) (string, error) {
	var lastErr error
	delay := 2 * time.Second
	for i := range maxAttempts {
		result, err := fn()
		if err == nil {
			return result, nil
		}
		lastErr = err
		if i < maxAttempts-1 {
			time.Sleep(delay)
			delay *= 2
		}
	}
	return "", lastErr
}

// RunPipeline executes all six email pipeline steps and returns the final Liquid output.
func RunPipeline(client *anthropicClient, input PipelineInput) (*PipelineResult, error) {
	var warnings []string

	// Step A: Define the CTA
	ctaBrief, err := stepA(client, input)
	if err != nil {
		return nil, fmt.Errorf("step A (CTA): %w", err)
	}

	// Step B: Write the storyselling email
	emailDraft, err := stepB(client, input, ctaBrief)
	if err != nil {
		return nil, fmt.Errorf("step B (write email): %w", err)
	}

	// Step C: Review the draft (up to 3 iterations)
	reviewedDraft, reviewWarning, err := stepC(client, input, emailDraft)
	if err != nil {
		return nil, fmt.Errorf("step C (review): %w", err)
	}
	if reviewWarning != "" {
		warnings = append(warnings, reviewWarning)
	}

	// Step D: Humanize the copy
	humanizedDraft, err := stepD(client, input, reviewedDraft)
	if err != nil {
		return nil, fmt.Errorf("step D (humanize): %w", err)
	}

	// Step E: Produce Liquid output
	liquidOutput, err := stepE(client, input, humanizedDraft, ctaBrief)
	if err != nil {
		return nil, fmt.Errorf("step E (liquid): %w", err)
	}

	// Step F: Fluency check (up to 2 iterations)
	finalLiquid, fluencyWarning, err := stepF(client, liquidOutput)
	if err != nil {
		return nil, fmt.Errorf("step F (fluency): %w", err)
	}
	if fluencyWarning != "" {
		warnings = append(warnings, fluencyWarning)
	}

	// Parse the three blocks from the final output
	title, preHeader, body := parseLiquidBlocks(finalLiquid)

	return &PipelineResult{
		TitleLiquid:     title,
		PreHeaderLiquid: preHeader,
		BodyLiquid:      body,
		Warnings:        warnings,
	}, nil
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

func stepA(client *anthropicClient, input PipelineInput) (string, error) {
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

	return client.Send(system, user)
}

func stepB(client *anthropicClient, input PipelineInput, ctaBrief string) (string, error) {
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
- Output: subject line, email body, and P.S. if applicable`,
		input.Context.Date,
		ctaBrief,
		input.OfferDescription,
		buildContextSummary(input),
	)

	return client.Send(system, user)
}

const (
	stepCMaxIterations = 3
	stepEMaxAttempts   = 2
	stepFMaxIterations = 2
)

func stepC(client *anthropicClient, input PipelineInput, draft string) (string, string, error) {
	system := strings.Join([]string{
		"# Brand Profile\n" + input.BrandProfile,
		"# Audience Segment\n" + input.AudienceSegment,
		"# Email Principles\n" + skillEmailPrinciples,
		"# Email Copy Review\n" + skillEmailCopyReview,
	}, "\n\n---\n\n")

	current := draft
	for range stepCMaxIterations {
		user := fmt.Sprintf(`Review the email draft below against all 14 checks.
For every issue found, provide a specific rewrite.

Then output the complete corrected email (subject line, body, and P.S. if any) between <revised_email> and </revised_email> tags. Put only the email inside the tags, with no review notes.
End your response with a final line: VERDICT: PASS or VERDICT: NEEDS REVISION

<email_draft>
%s
</email_draft>`, current)

		result, err := client.Send(system, user)
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

func stepD(client *anthropicClient, input PipelineInput, draft string) (string, error) {
	system := strings.Join([]string{
		"# Brand Profile (Voice Section)\n" + input.BrandProfile,
		"# Humanize Copy Rules\n" + skillHumanizeCopy,
	}, "\n\n---\n\n")

	user := fmt.Sprintf(`Review the email below for AI writing artefacts and humanize it.
Remove patterns that mark it as machine-generated. Preserve meaning, structure, and brand voice.
Output the full revised copy between <humanized_email> and </humanized_email> tags, with no annotations or commentary inside the tags.

<email>
%s
</email>`, draft)

	result, err := client.Send(system, user)
	if err != nil {
		return "", err
	}
	if humanized, ok := extractTagged(result, "humanized_email"); ok {
		return humanized, nil
	}
	return strings.TrimSpace(result), nil
}

func stepE(client *anthropicClient, input PipelineInput, draft, ctaBrief string) (string, error) {
	system := strings.Join([]string{
		"# Brand Profile\n" + input.BrandProfile,
		"# Liquid Output Rules\n" + skillEmailLiquidOutput,
	}, "\n\n---\n\n")

	user := fmt.Sprintf(`Convert the email below into production-ready Liquid code for UserList.

Produce three blocks:
1. Title (subject line)
2. Pre-header
3. Message body

Each block must contain all six languages (French, Spanish, Brazilian Portuguese, Italian, Japanese, English) using a case switch on user.language. The else branch is English.

CTA Brief (for URL and anchor text):
%s

Campaign slug: daily-nurture
Content slug: derive from the email story topic in kebab-case

Apply all formatting rules:
- Single carriage returns only
- Primary CTA on its own line with no trailing period
- Signature snippet at the end: {{snippets.en_signature_sebastien}}
- UTM parameters on every link
- Localized time and currency
- Informal register in all languages

Output format: put the three blocks between <liquid_email> and </liquid_email> tags. Inside the tags, each block starts with its heading (### Title, ### Pre-header, ### Message body) followed by its Liquid code in a `+"```liquid"+` fenced code block. Nothing else goes inside the tags.

<email>
%s
</email>`,
		ctaBrief,
		draft,
	)

	var lastErr error
	for range stepEMaxAttempts {
		result, err := client.Send(system, user)
		if err != nil {
			return "", err
		}
		liquid, ok := extractTagged(result, "liquid_email")
		if !ok {
			liquid = strings.TrimSpace(result)
		}
		if err := validateLiquid(liquid); err != nil {
			lastErr = err
			continue
		}
		return liquid, nil
	}
	return "", fmt.Errorf("output is not usable Liquid after %d attempts: %w", stepEMaxAttempts, lastErr)
}

// stepF runs the fluency check. liquidOutput must already be valid Liquid (from step E);
// a fluency pass whose output fails validation is discarded, so the result is always valid.
func stepF(client *anthropicClient, liquidOutput string) (string, string, error) {
	system := "# Email Fluency Check Rules\n" + skillEmailFluencyCheck

	current := liquidOutput
	rejected := false
	for range stepFMaxIterations {
		user := fmt.Sprintf(`Review all non-English language versions in the Liquid email below for native fluency.
Flag calques, anglicisms, register drift, or sentence rhythm that betrays translation.
For each issue, provide a native rewrite.

Then output the complete corrected Liquid between <final_liquid> and </final_liquid> tags, keeping the exact same structure: the ### Title, ### Pre-header and ### Message body headings, each followed by its `+"```liquid"+` fenced code block. Nothing else goes inside the tags.
End your response with a final line: VERDICT: READY FOR PRODUCTION or VERDICT: REVISIONS REQUIRED

<liquid_email>
%s
</liquid_email>`, current)

		result, err := client.Send(system, user)
		if err != nil {
			return current, "", err
		}

		if candidate, ok := extractTagged(result, "final_liquid"); ok && validateLiquid(candidate) == nil {
			current = candidate
		} else {
			rejected = true
		}

		verdict := finalVerdict(result)
		if strings.Contains(verdict, "READY FOR PRODUCTION") && !strings.Contains(verdict, "REVISIONS REQUIRED") {
			if rejected {
				return current, "⚠️ Fluency check returned unusable output at least once; kept the last valid Liquid version.", nil
			}
			return current, "", nil
		}
	}
	return current, fmt.Sprintf("⚠️ Fluency check loop reached maximum iterations (%d). Proceeding with best available version.", stepFMaxIterations), nil
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

var liquidCaseRe = regexp.MustCompile(`\{%-?\s*case\s+user\.language\s*-?%\}`)

// validateLiquid checks that s is a Liquid email with a language switch and
// parseable Title and Message body blocks.
func validateLiquid(s string) error {
	if !liquidCaseRe.MatchString(s) || !strings.Contains(s, "endcase") {
		return fmt.Errorf("missing {%% case user.language %%} switch")
	}
	title, _, body := parseLiquidBlocks(s)
	if title == "" || body == "" || body == strings.TrimSpace(s) {
		return fmt.Errorf("missing ### Title or ### Message body block")
	}
	return nil
}

// parseLiquidBlocks extracts the three ### Title / ### Pre-header / ### Message body sections
// from the pipeline's final output text.
func parseLiquidBlocks(output string) (title, preHeader, body string) {
	sections := map[string]*string{
		"title":          &title,
		"pre-header":     &preHeader,
		"pre header":     &preHeader,
		"message body":   &body,
		"message-body":   &body,
	}

	lines := strings.Split(output, "\n")
	var currentSection *string
	var currentContent strings.Builder
	inCodeBlock := false

	flush := func() {
		if currentSection != nil {
			content := strings.TrimSpace(currentContent.String())
			// Strip surrounding ```liquid ... ``` fences if present
			content = strings.TrimPrefix(content, "```liquid")
			content = strings.TrimPrefix(content, "```")
			content = strings.TrimSuffix(content, "```")
			*currentSection = strings.TrimSpace(content)
			currentContent.Reset()
		}
	}

	for _, line := range lines {
		lower := strings.ToLower(strings.TrimSpace(line))

		// Headings inside a code fence are email content (the body may use ## headings).
		if !inCodeBlock && strings.HasPrefix(lower, "##") {
			heading := strings.TrimSpace(strings.TrimLeft(lower, "#"))
			flush()
			currentSection = nil
			for key, ptr := range sections {
				if strings.HasPrefix(heading, key) {
					currentSection = ptr
					break
				}
			}
			continue
		}

		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inCodeBlock = !inCodeBlock
		}

		if currentSection != nil {
			currentContent.WriteString(line + "\n")
		}
	}
	flush()

	// Fallback: if parsing failed, put everything in the body
	if title == "" && preHeader == "" && body == "" {
		body = output
	}

	return title, preHeader, body
}
