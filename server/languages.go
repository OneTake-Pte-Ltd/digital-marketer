package main

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// emailLanguage is one language the email is sent in. An empty code is the case switch's
// else branch.
type emailLanguage struct {
	code string
	name string
}

// emailLanguages lists the languages in case-switch order. French is the source language;
// English is the else branch, read by everyone whose language is not listed.
var emailLanguages = []emailLanguage{
	{"fr", "French"},
	{"es", "Spanish"},
	{"pt", "Brazilian Portuguese"},
	{"it", "Italian"},
	{"ja", "Japanese"},
	{"", "English"},
}

// required reports whether the run fails when this language fails. Other languages fall
// back to the English branch.
func (l emailLanguage) required() bool { return l.code == "fr" || l.code == "" }

func (l emailLanguage) slug() string {
	if l.code == "" {
		return "en"
	}
	return l.code
}

// languageVersion is the email in one language, before Neil assembles the Liquid.
type languageVersion struct {
	Title     string
	PreHeader string
	Body      string
	PS        string
}

const signatureSnippet = "{{snippets.en_signature_sebastien}}"

// parseLanguageVersion reads a version from its <title>, <preheader>, <body> and <ps>
// tags. A title and a body are required.
func parseLanguageVersion(s string) (languageVersion, bool) {
	title, okTitle := extractTagged(s, "title")
	body, okBody := extractTagged(s, "body")
	if !okTitle || !okBody {
		return languageVersion{}, false
	}
	preHeader, _ := extractTagged(s, "preheader")
	ps, _ := extractTagged(s, "ps")
	// Neil adds the signature itself, and UserList wants no empty lines.
	body = removeBlankLines(strings.ReplaceAll(body, signatureSnippet, ""))
	ps = removeBlankLines(strings.ReplaceAll(ps, signatureSnippet, ""))
	return languageVersion{
		Title:     singleLine(title),
		PreHeader: singleLine(preHeader),
		Body:      body,
		PS:        ps,
	}, true
}

// tagged renders the version in the tag format the prompts use.
func (v languageVersion) tagged() string {
	s := fmt.Sprintf("<title>%s</title>\n<preheader>%s</preheader>\n<body>\n%s\n</body>", v.Title, v.PreHeader, v.Body)
	if v.PS != "" {
		s += fmt.Sprintf("\n<ps>%s</ps>", v.PS)
	}
	return s
}

var blankLinesRe = regexp.MustCompile(`\n\s*\n`)

func removeBlankLines(s string) string {
	return strings.TrimSpace(blankLinesRe.ReplaceAllString(strings.TrimSpace(s), "\n"))
}

// singleLine joins lines into one line, for UserList's one-line title and pre-header
// fields. A line break next to a Liquid tag carries no content and is dropped; one between
// two lines of text becomes a space.
func singleLine(text string) string {
	var sb strings.Builder
	prev := ""
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if prev != "" && !strings.HasSuffix(prev, "%}") && !strings.HasPrefix(line, "{%") {
			sb.WriteString(" ")
		}
		sb.WriteString(line)
		prev = line
	}
	return sb.String()
}

// translateAll writes every language version of the draft at the same time: step E
// (translation and formatting) then, except for the French source, step F (fluency check).
// A language that fails falls back to English with a warning, unless it is required.
func translateAll(ctx context.Context, client *anthropicClient, input PipelineInput, draft, ctaBrief string, report pipelineReporter) (map[string]languageVersion, []string, error) {
	type outcome struct {
		version languageVersion
		warning string
		err     error
	}
	outcomes := make([]outcome, len(emailLanguages))

	var wg sync.WaitGroup
	var reportMu sync.Mutex
	for i, lang := range emailLanguages {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := stepE(ctx, client, input, lang, draft, ctaBrief)
			if err != nil {
				outcomes[i].err = fmt.Errorf("step E (%s): %w", lang.name, err)
				return
			}
			warning := ""
			if lang.code != "fr" {
				v, warning = stepF(ctx, client, lang, draft, v)
			}
			outcomes[i] = outcome{version: v, warning: warning}

			reportMu.Lock()
			defer reportMu.Unlock()
			report.result("lang-"+lang.slug(), "Steps E and F: "+lang.name+" version", v.tagged(), false)
		}()
	}
	wg.Wait()

	versions := map[string]languageVersion{}
	var warnings []string
	for i, lang := range emailLanguages {
		o := outcomes[i]
		if o.err != nil {
			if lang.required() {
				return nil, nil, o.err
			}
			warnings = append(warnings, fmt.Sprintf("⚠️ The %s version failed (%s); %s readers get the English version.", lang.name, o.err.Error(), lang.name))
			continue
		}
		versions[lang.code] = o.version
		if o.warning != "" {
			warnings = append(warnings, o.warning)
		}
	}
	return versions, warnings, nil
}

// assembleEmail builds the Liquid title, pre-header and body from the language versions.
// A language missing from versions is left out of the switch, so its readers get the
// English else branch.
func assembleEmail(versions map[string]languageVersion) (title, preHeader, body string) {
	hasPS := false
	for _, v := range versions {
		if v.PS != "" {
			hasPS = true
		}
	}

	var t, p, b strings.Builder
	t.WriteString("{%case user.language%}")
	p.WriteString("{%case user.language%}")
	b.WriteString("{%case user.language%}\n")
	for _, lang := range emailLanguages {
		v, ok := versions[lang.code]
		if !ok {
			continue
		}
		branch := fmt.Sprintf("{%% when '%s' %%}", lang.code)
		if lang.code == "" {
			branch = "{% else %}"
		}
		t.WriteString(branch + v.Title)
		p.WriteString(branch + v.PreHeader)
		b.WriteString(branch + "\n" + v.Body + "\n")
		// With a P.S., the signature goes before it, inside each language.
		if hasPS {
			b.WriteString(signatureSnippet + "\n")
			if v.PS != "" {
				b.WriteString(v.PS + "\n")
			}
		}
	}
	t.WriteString("{% endcase %}")
	p.WriteString("{% endcase %}")
	b.WriteString("{% endcase %}")
	if !hasPS {
		b.WriteString("\n" + signatureSnippet)
	}
	return t.String(), p.String(), b.String()
}

func stepE(ctx context.Context, client *anthropicClient, input PipelineInput, lang emailLanguage, draft, ctaBrief string) (languageVersion, error) {
	system := strings.Join([]string{
		"# Brand Profile\n" + input.BrandProfile,
		"# Language Version Rules\n" + skillEmailLanguageVersion,
	}, "\n\n---\n\n")

	task := fmt.Sprintf("Translate it from French into %s, as a native %s speaker would write it, not word for word.", lang.name, lang.name)
	if lang.code == "fr" {
		task = "The email is already in French: keep its wording and apply the formatting, link and localization rules."
	}

	user := fmt.Sprintf(`Write the %s version of the email below for UserList. %s

CTA Brief (for URL and anchor text):
%s

Campaign slug: daily-nurture
Content slug: email-%s

Apply all the rules for %s:
- Single carriage returns only
- Primary CTA on its own line with no trailing period
- UTM parameters on every link
- Localized time and currency
- Informal register
- Active voice and full sentences, as in the source
- No placeholder or bracketed note
- The title and the pre-header are one line each
- No signature snippet: Neil adds it

Output exactly this, with nothing else inside the tags:
<version>
<title>the subject line</title>
<preheader>the pre-header</preheader>
<body>
the message body
</body>
<ps>the P.S., only if the source email has one</ps>
</version>

<email>
%s
</email>`, lang.name, task, ctaBrief, input.Context.Date, lang.name, draft)

	for range stepEMaxAttempts {
		result, err := client.Send(ctx, system, user)
		if err != nil {
			return languageVersion{}, err
		}
		if v, ok := parseLanguageVersion(result); ok {
			return v, nil
		}
	}
	return languageVersion{}, fmt.Errorf("no usable <title> and <body> after %d attempts", stepEMaxAttempts)
}

// stepF checks the fluency of one translated version, up to stepFMaxIterations times. It
// always returns a usable version: a pass whose output cannot be read is discarded.
func stepF(ctx context.Context, client *anthropicClient, lang emailLanguage, frenchDraft string, version languageVersion) (languageVersion, string) {
	system := "# Email Fluency Check Rules\n" + skillEmailFluencyCheck

	current := version
	rejected := false
	for range stepFMaxIterations {
		user := fmt.Sprintf(`Review the %s version below for native fluency. The French original is given for its meaning only.
Flag calques, anglicisms, register drift, or sentence rhythm that betrays translation. For each issue, provide a native rewrite.
Keep links, UTM parameters, Markdown and Liquid variables exactly as they are. Keep the title and the pre-header on one line each.

Then output the complete corrected version between <final_version> and </final_version>, with the same <title>, <preheader>, <body> and <ps> tags.
End your response with a final line: VERDICT: READY FOR PRODUCTION or VERDICT: REVISIONS REQUIRED

<french_original>
%s
</french_original>

<version>
%s
</version>`, lang.name, frenchDraft, current.tagged())

		result, err := client.Send(ctx, system, user)
		if err != nil {
			if errors.Is(err, errAnswerCutOff) {
				rejected = true
				continue
			}
			return current, fmt.Sprintf("⚠️ The %s fluency check failed (%s); kept the version before it.", lang.name, err.Error())
		}

		if final, ok := extractTagged(result, "final_version"); ok {
			if v, ok := parseLanguageVersion(final); ok {
				current = v
			} else {
				rejected = true
			}
		} else {
			rejected = true
		}

		verdict := finalVerdict(result)
		if strings.Contains(verdict, "READY FOR PRODUCTION") && !strings.Contains(verdict, "REVISIONS REQUIRED") {
			if rejected {
				return current, fmt.Sprintf("⚠️ The %s fluency check returned unusable output at least once; kept the last usable version.", lang.name)
			}
			return current, ""
		}
	}
	return current, fmt.Sprintf("⚠️ The %s fluency check reached its maximum of %d passes; kept the best version.", lang.name, stepFMaxIterations)
}
