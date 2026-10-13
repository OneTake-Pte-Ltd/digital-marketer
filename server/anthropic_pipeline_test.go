package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validLiquid = "### Title\n```liquid\n{%case user.language%}\n{% when 'fr' %}\nBonjour\n{% else %}\nHello\n{% endcase %}\n```\n\n" +
	"### Pre-header\n```liquid\n{%case user.language%}\n{% when 'fr' %}\nAperçu\n{% else %}\nPreview\n{% endcase %}\n```\n\n" +
	"### Message body\n```liquid\n{%case user.language%}\n{% when 'fr' %}\nCorps\n## Un sous-titre\nSuite du corps\n{% else %}\nBody\n## A subheading\nMore body\n{% endcase %}\n{{snippets.en_signature_sebastien}}\n```"

// fakeAnthropic serves the given replies in order, one per request, and records the prompts.
func fakeAnthropic(t *testing.T, replies ...string) (*anthropicClient, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var prompts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req anthropicRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		mu.Lock()
		defer mu.Unlock()
		prompts = append(prompts, req.Messages[0].Content)
		require.LessOrEqual(t, len(prompts), len(replies), "unexpected extra API call")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]string{{"type": "text", "text": replies[len(prompts)-1]}},
		})
	}))
	t.Cleanup(srv.Close)
	c := newAnthropicClient("key", "model")
	c.endpoint = srv.URL
	return c, &prompts
}

func TestExtractTagged(t *testing.T) {
	got, ok := extractTagged("notes\n<x>\n  content\n</x>\nVERDICT: PASS", "x")
	assert.True(t, ok)
	assert.Equal(t, "content", got)

	_, ok = extractTagged("no tags here", "x")
	assert.False(t, ok)

	_, ok = extractTagged("<x>   </x>", "x")
	assert.False(t, ok, "empty content is not a usable extraction")
}

func TestFinalVerdict(t *testing.T) {
	assert.Equal(t, "PASS", finalVerdict("Check 1: NEEDS REVISION\n...\nVerdict: pass"))
	assert.Equal(t, "REVISIONS REQUIRED", finalVerdict("Overall verdict: READY FOR PRODUCTION\nVERDICT: REVISIONS REQUIRED"))
	assert.Equal(t, "", finalVerdict("no verdict"))
}

func TestValidateLiquid(t *testing.T) {
	assert.NoError(t, validateLiquid(validLiquid))
	assert.Error(t, validateLiquid("You submitted the skill definition itself as the email to review."))
	assert.Error(t, validateLiquid("{%case user.language%}{% endcase %}"), "no blocks")
}

func TestParseLiquidBlocksKeepsBodyHeadings(t *testing.T) {
	title, preHeader, body := parseLiquidBlocks(validLiquid)
	assert.Contains(t, title, "Bonjour")
	assert.Contains(t, preHeader, "Preview")
	assert.Contains(t, body, "## Un sous-titre\nSuite du corps")
	assert.Contains(t, body, "{{snippets.en_signature_sebastien}}")
	assert.False(t, strings.HasPrefix(body, "```"))
}

// Reproduces the 2026-10-13 failure: the fluency check replied with a complaint
// instead of Liquid. The step E Liquid must be kept, never the complaint.
func TestStepFKeepsValidLiquidWhenReviewerReturnsGarbage(t *testing.T) {
	complaint := "You've caught something worth naming directly.\n## What Just Happened\nThere is nothing to fluency-check."
	client, prompts := fakeAnthropic(t, complaint, complaint)

	out, warning, err := stepF(client, validLiquid)
	require.NoError(t, err)
	assert.Equal(t, validLiquid, out)
	assert.Contains(t, warning, "maximum iterations")
	require.Len(t, *prompts, 2)
	// The second pass reviews the Liquid again, not the first reply.
	assert.Contains(t, (*prompts)[1], validLiquid)
	assert.NotContains(t, (*prompts)[1], "worth naming directly")
}

func TestStepFUsesCorrectedLiquid(t *testing.T) {
	corrected := strings.Replace(validLiquid, "Corps", "Corps corrigé", 1)
	client, _ := fakeAnthropic(t, "French: NEEDS REVISION\n<final_liquid>\n"+corrected+"\n</final_liquid>\nVERDICT: READY FOR PRODUCTION")

	out, warning, err := stepF(client, validLiquid)
	require.NoError(t, err)
	assert.Equal(t, corrected, out)
	assert.Empty(t, warning)
}

func TestStepCCarriesOnlyTheRevisedEmail(t *testing.T) {
	client, prompts := fakeAnthropic(t,
		"Check 1 failed: weak subject.\n<revised_email>\nObjet : version 2\n</revised_email>\nVERDICT: NEEDS REVISION",
		"All good.\n<revised_email>\nObjet : version 3\n</revised_email>\nVERDICT: PASS",
	)

	out, warning, err := stepC(client, PipelineInput{}, "Objet : version 1")
	require.NoError(t, err)
	assert.Equal(t, "Objet : version 3", out)
	assert.Empty(t, warning)
	require.Len(t, *prompts, 2)
	assert.Contains(t, (*prompts)[1], "Objet : version 2")
	assert.NotContains(t, (*prompts)[1], "weak subject")
}

func TestStepERejectsNonLiquid(t *testing.T) {
	client, _ := fakeAnthropic(t, "Please paste the email.", "Still no email.")
	_, err := stepE(client, PipelineInput{}, "draft", "brief")
	assert.Error(t, err)
}

func TestStepERetriesOnce(t *testing.T) {
	client, _ := fakeAnthropic(t, "Please paste the email.", "<liquid_email>\n"+validLiquid+"\n</liquid_email>")
	out, err := stepE(client, PipelineInput{}, "draft", "brief")
	require.NoError(t, err)
	assert.Equal(t, validLiquid, out)
}
