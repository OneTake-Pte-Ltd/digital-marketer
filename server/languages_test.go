package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeAnthropicByPrompt answers each request with reply(user prompt). It suits steps that
// run at the same time, where the order of requests is not fixed.
func fakeAnthropicByPrompt(t *testing.T, reply func(user string) string) *anthropicClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req anthropicRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]string{{"type": "text", "text": reply(req.Messages[0].Content)}},
		})
	}))
	t.Cleanup(srv.Close)
	c := newAnthropicClient("key", "model")
	c.endpoint = srv.URL
	return c
}

// reportRecorder collects what RunPipeline reports.
type reportRecorder struct {
	mu      sync.Mutex
	steps   []string
	results map[string]string
}

func (r *reportRecorder) step(name string) { r.steps = append(r.steps, name) }
func (r *reportRecorder) result(slug, _, text string, _ bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.results == nil {
		r.results = map[string]string{}
	}
	r.results[slug] = text
}

func versionReply(title, body string) string {
	return "<version>\n<title>" + title + "</title>\n<preheader>Aperçu " + title + "</preheader>\n<body>\n" + body + "\n</body>\n</version>"
}

func TestParseLanguageVersion(t *testing.T) {
	v, ok := parseLanguageVersion("Notes\n<version>\n<title>100 % de bénéfice :\nla ceinture blanche</title>\n<preheader>Aperçu</preheader>\n<body>\nLigne 1\n\nLigne 2\n{{snippets.en_signature_sebastien}}\n</body>\n</version>")
	require.True(t, ok)
	assert.Equal(t, "100 % de bénéfice : la ceinture blanche", v.Title)
	assert.Equal(t, "Ligne 1\nLigne 2", v.Body, "no blank lines and no signature: Neil adds it")
	assert.Empty(t, v.PS)

	_, ok = parseLanguageVersion("<title>Titre</title>")
	assert.False(t, ok, "a body is required")
}

func TestAssembleEmailWithoutPS(t *testing.T) {
	title, preHeader, body := assembleEmail(map[string]languageVersion{
		"fr": {Title: "Titre", PreHeader: "Aperçu", Body: "Corps"},
		"":   {Title: "Title", PreHeader: "Preview", Body: "Body"},
	})
	assert.Equal(t, "{%case user.language%}{% when 'fr' %}Titre{% else %}Title{% endcase %}", title)
	assert.Equal(t, "{%case user.language%}{% when 'fr' %}Aperçu{% else %}Preview{% endcase %}", preHeader)
	assert.Equal(t, "{%case user.language%}\n{% when 'fr' %}\nCorps\n{% else %}\nBody\n{% endcase %}\n{{snippets.en_signature_sebastien}}", body)
}

func TestAssembleEmailWithPS(t *testing.T) {
	_, _, body := assembleEmail(map[string]languageVersion{
		"fr": {Title: "Titre", Body: "Corps", PS: "P.S. Bonus"},
		"":   {Title: "Title", Body: "Body", PS: "P.S. Bonus"},
	})
	assert.Equal(t, "{%case user.language%}\n{% when 'fr' %}\nCorps\n{{snippets.en_signature_sebastien}}\nP.S. Bonus\n{% else %}\nBody\n{{snippets.en_signature_sebastien}}\nP.S. Bonus\n{% endcase %}", body)
}

func TestStepERetriesOnce(t *testing.T) {
	client, prompts := fakeAnthropic(t, "Please paste the email.", versionReply("Título", "Cuerpo"))
	v, err := stepE(context.Background(), client, PipelineInput{}, emailLanguages[1], "Objet : brouillon", "brief")
	require.NoError(t, err)
	assert.Equal(t, "Cuerpo", v.Body)
	assert.Contains(t, (*prompts)[0], "Write the Spanish version")
	assert.Contains(t, (*prompts)[0], "Objet : brouillon")
}

func TestStepFKeepsVersionWhenReviewerReturnsGarbage(t *testing.T) {
	complaint := "There is nothing to fluency-check.\nVERDICT: REVISIONS REQUIRED"
	client, _ := fakeAnthropic(t, complaint, complaint)
	original := languageVersion{Title: "Título", Body: "Cuerpo"}

	v, warning := stepF(context.Background(), client, emailLanguages[1], "Objet", original)
	assert.Equal(t, original, v)
	assert.Contains(t, warning, "Spanish")
}

func TestStepFUsesCorrectedVersion(t *testing.T) {
	client, prompts := fakeAnthropic(t, "Notes\n<final_version>"+versionReply("Título", "Cuerpo corregido")+"</final_version>\nVERDICT: READY FOR PRODUCTION")

	v, warning := stepF(context.Background(), client, emailLanguages[1], "Objet : original", languageVersion{Title: "Título", Body: "Cuerpo"})
	assert.Equal(t, "Cuerpo corregido", v.Body)
	assert.Empty(t, warning)
	assert.Contains(t, (*prompts)[0], "Objet : original", "the French original is given for meaning")
}

// pipelineReplies answers steps A to D, and steps E and F for every language except the
// ones in failing, whose step E never returns a usable version.
func pipelineReplies(failing ...string) func(user string) string {
	return func(user string) string {
		for _, lang := range emailLanguages {
			if strings.HasPrefix(user, "Write the "+lang.name+" version") {
				for _, f := range failing {
					if f == lang.name {
						return "I can't."
					}
				}
				return versionReply("Title "+lang.name, "Body "+lang.name+" [à confirmer]")
			}
			if strings.HasPrefix(user, "Review the "+lang.name+" version") {
				return "<final_version>" + versionReply("Title "+lang.name, "Body "+lang.name+" [à confirmer]") + "</final_version>\nVERDICT: READY FOR PRODUCTION"
			}
		}
		switch {
		case strings.HasPrefix(user, "Review the email draft"):
			return "<revised_email>Objet : revu</revised_email>\nVERDICT: PASS"
		case strings.Contains(user, "humanize it"):
			return "<humanized_email>Objet : humain</humanized_email>"
		case strings.Contains(user, "Write a storyselling"):
			return "Objet : brouillon"
		default:
			return "CTA brief"
		}
	}
}

func TestRunPipelineAssemblesEveryLanguage(t *testing.T) {
	rec := &reportRecorder{}
	result, err := RunPipeline(context.Background(), fakeAnthropicByPrompt(t, pipelineReplies("Italian")), PipelineInput{}, rec)
	require.NoError(t, err)

	assert.NotContains(t, result.TitleLiquid, "\n")
	assert.Contains(t, result.TitleLiquid, "{% when 'ja' %}Title Japanese{% else %}Title English{% endcase %}")
	assert.NotContains(t, result.BodyLiquid, "{% when 'it' %}", "a failed language falls back to the English branch")
	assert.Contains(t, strings.Join(result.Warnings, "\n"), "Italian readers get the English version")
	assert.Contains(t, strings.Join(result.Warnings, "\n"), "[à confirmer]")

	assert.Equal(t, "Objet : humain", rec.results["step-d-humanized"])
	assert.Contains(t, rec.results["lang-es"], "Body Spanish")
	assert.Contains(t, rec.results["lang-en"], "Body English")
	assert.NotContains(t, rec.results, "lang-it")
}

func TestRunPipelineFailsWithoutFrench(t *testing.T) {
	_, err := RunPipeline(context.Background(), fakeAnthropicByPrompt(t, pipelineReplies("French")), PipelineInput{}, &reportRecorder{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "step E (French)")
}
