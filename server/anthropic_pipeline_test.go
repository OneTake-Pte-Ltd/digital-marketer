package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func TestStepCCarriesOnlyTheRevisedEmail(t *testing.T) {
	client, prompts := fakeAnthropic(t,
		"Check 1 failed: weak subject.\n<revised_email>\nObjet : version 2\n</revised_email>\nVERDICT: NEEDS REVISION",
		"All good.\n<revised_email>\nObjet : version 3\n</revised_email>\nVERDICT: PASS",
	)

	out, warning, err := stepC(context.Background(), client, PipelineInput{}, "Objet : version 1")
	require.NoError(t, err)
	assert.Equal(t, "Objet : version 3", out)
	assert.Empty(t, warning)
	require.Len(t, *prompts, 2)
	assert.Contains(t, (*prompts)[1], "Objet : version 2")
	assert.NotContains(t, (*prompts)[1], "weak subject")
}

// fakeAnthropicStatus answers every request with the given status code and body, and counts calls.
func fakeAnthropicStatus(t *testing.T, code int, body string) (*anthropicClient, *int) {
	t.Helper()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c := newAnthropicClient("key", "model")
	c.endpoint = srv.URL
	return c, &calls
}

func TestSendDoesNotRetryCutOffAnswer(t *testing.T) {
	client, calls := fakeAnthropicStatus(t, http.StatusOK, `{"stop_reason":"max_tokens","content":[{"type":"text","text":"half an ema"}]}`)
	_, err := client.Send(context.Background(), "system", "user")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cut off")
	assert.Equal(t, 1, *calls)
}

func TestSendDoesNotRetryClientError(t *testing.T) {
	client, calls := fakeAnthropicStatus(t, http.StatusBadRequest, `{"error":{"message":"bad request"}}`)
	_, err := client.Send(context.Background(), "system", "user")
	require.Error(t, err)
	assert.Equal(t, 1, *calls)
}

func TestSendStopsRetryingWhenContextEnds(t *testing.T) {
	client, calls := fakeAnthropicStatus(t, http.StatusServiceUnavailable, "overloaded")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := client.Send(ctx, "system", "user")
	require.Error(t, err)
	assert.LessOrEqual(t, *calls, 1)
}

func TestSendSkipsThinkingBlocks(t *testing.T) {
	client, _ := fakeAnthropicStatus(t, http.StatusOK, `{"stop_reason":"end_turn","content":[{"type":"thinking","thinking":""},{"type":"text","text":"Objet : bonjour"}]}`)
	out, err := client.Send(context.Background(), "system", "user")
	require.NoError(t, err)
	assert.Equal(t, "Objet : bonjour", out)
}

func TestSendRejectsAnswerWithoutText(t *testing.T) {
	client, _ := fakeAnthropicStatus(t, http.StatusOK, `{"stop_reason":"end_turn","content":[{"type":"thinking","thinking":""}]}`)
	_, err := client.Send(context.Background(), "system", "user")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no text")
}

func TestSendDoesNotRetryRefusal(t *testing.T) {
	client, calls := fakeAnthropicStatus(t, http.StatusOK, `{"stop_reason":"refusal","content":[]}`)
	_, err := client.Send(context.Background(), "system", "user")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "declined")
	assert.Equal(t, 1, *calls)
}

func TestSingleLine(t *testing.T) {
	in := "{%case user.language%}\n{% when 'fr' %}\n100 % de bénéfice :\nla ceinture blanche\n{% else %}\n100% profit\n{% endcase %}"
	assert.Equal(t, "{%case user.language%}{% when 'fr' %}100 % de bénéfice : la ceinture blanche{% else %}100% profit{% endcase %}", singleLine(in))
}

func TestFindPlaceholders(t *testing.T) {
	text := "Suzanne Garrett. [Trait physique à confirmer par Sébastien avant l'envoi.] " +
		"[Clique ici pour voir la vidéo](https://onetake.ai/?utm_source={{user.utm_source}}) " +
		"{%case user.language%} [Trait physique à confirmer par Sébastien avant l'envoi.] [TODO]"
	assert.Equal(t, []string{"[Trait physique à confirmer par Sébastien avant l'envoi.]", "[TODO]"}, findPlaceholders(text))
}
