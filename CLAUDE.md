# Neil — Architecture Reference

This file is the primary reference for AI-assisted maintenance of the Neil Mattermost plugin. Read it before modifying any server-side code.

## Overview

Neil is a Mattermost plugin (Go, server-only logic with a minimal webapp for settings). It has three subsystems:

1. **Telegram Listener** — receives channel posts via webhook and stores them in the Mattermost KV store.
2. **Morning Processor** — runs at 06:00 CET, reads yesterday's posts, transcribes media, runs the email pipeline, and posts results to Mattermost.
3. **Mattermost Interface** — slash command, mention trigger, and bot user for posting.

There is no external database. All state lives in the Mattermost plugin KV store.

## File Map

```
server/
  main.go                   Entry point. Imports _ "time/tzdata" for embedded CET timezone data.
  plugin.go                 Plugin struct, OnActivate, OnDeactivate, ExecuteCommand,
                            MessageHasBeenPosted, ServeHTTP, postToChannel, registerWebhook.
  api.go                    HTTP router. /telegram/webhook (no auth), /api/v1/* (auth-protected).
  configuration.go          Plugin settings struct, getConfiguration/setConfiguration,
                            OnConfigurationChange (re-registers Telegram webhook on save).
  job.go                    Hourly cluster job; fires processor at 06:00 CET, skips if already run.
  processor.go              RunProcessor: Steps 2.1–2.7 orchestration. Also contains CET helpers,
                            loadAssets, transcribeMediaPost, uploadMedia, buildDayContext,
                            postEmailResult, handleProcessorError, extensionForContentType.
  types.go                  All shared structs: TelegramPost, MediaRecord, RunStatus, DayContext,
                            PipelineInput, PipelineResult, raw Telegram API types.
  skills.go                 Eight Go string constants — one per prompt skill (email principles,
                            CTA, nurture, copy review, humanize copy, Liquid output, fluency check,
                            storyselling framework).
  telegram.go               telegramClient: SetWebhook, DeleteWebhook, GetFileURL, DownloadFile,
                            parseUpdate (converts raw Telegram update to TelegramPost).
  bunnycdn.go               bunnyCDNClient: UploadFile — PUT to Bunny Edge Script proxy,
                            returns JSON {"url": "..."}. Retries network errors and 5xx
                            (3 attempts); summarizeErrorBody shortens HTML error pages.
  elevenlabs.go             elevenLabsClient: Transcribe — multipart POST to ElevenLabs Scribe v2
                            using the public CDN URL.
  anthropic_pipeline.go     anthropicClient (Send with 3-retry backoff), RunPipeline (A–F),
                            stepA–stepF functions, buildContextSummary, parseLiquidBlocks,
                            extractTagged, finalVerdict, validateLiquid.
  plugin_test.go            Smoke test: router initializes without panic.
  anthropic_pipeline_test.go  Steps C/E/F against a fake Anthropic server; Liquid parsing.
  media_test.go             Bunny retries, token redaction, caption URLs, buildDayContext.

  command/
    command.go              /neil run [YYYY-MM-DD] slash command. Accepts Runner interface
                            (implemented by Plugin) to call RunProcessor.
    command_test.go         Unit tests for command parsing.
    mocks/mock_commands.go  MockCommand for use in plugin-level tests.

  store/kvstore/
    kvstore.go              KVStore interface (10 methods).
    neil.go                 Client implementation using pluginapi KV. Key format functions.

assets/
  brand-profile.md          Sender identity, voice/tone rules, UTM defaults. Read at runtime.
  audience-segment.md       Segment definition: opt-in origin, awareness level, pain, 3AM question.
  avatar.md                 Laurent avatar — detailed psychographic profile.

edge-scripts/
  neil-upload.js            Bunny Edge Script. Validates Authorization header, proxies PUT to
                            BunnyCDN Storage Zone, returns {"url": "..."}.
```

## KV Store Key Schema

| Key | Value | Written by | Read by |
|---|---|---|---|
| `tg:post:{YYYY-MM-DD}:{message_id}` | `TelegramPost` JSON | Webhook handler | RunProcessor |
| `tg:index:{YYYY-MM-DD}` | `[]int64` (ordered message IDs) | Webhook handler | RunProcessor |
| `run:status:{YYYY-MM-DD}` | `RunStatus` JSON | RunProcessor | runJob (duplicate guard) |
| `media:{message_id}` | `MediaRecord` JSON (CDN URL + transcription) | transcribeMediaPost | transcribeMediaPost (reuses stored work on replay) |
| `neil:webhook:secret` | string (hex-encoded 32 bytes) | registerWebhook (once) | handleTelegramWebhook |

The index key uses a 5-attempt optimistic CAS loop (get-modify-set) in `AppendToIndex` to handle concurrent webhook deliveries safely.

## Configuration Fields

All fields map 1:1 to `plugin.json` settings. Access via `p.getConfiguration()` anywhere in the server package.

| Field | Type | Default | Notes |
|---|---|---|---|
| `BotDisplayName` | string | `"Neil"` | Bot user display name on Mattermost |
| `ChannelID` | string | | Required; channel where Neil posts results |
| `UsersToTag` | string | | Comma-separated usernames; @-mentioned in each post |
| `TelegramChannelUsername` | string | | Used for documentation only; not used in code |
| `TelegramBotToken` | string | | Required; triggers webhook registration on change |
| `BunnyCDNEdgeScriptURL` | string | | Base URL for the Bunny Edge Script proxy |
| `BunnyCDNEdgeScriptSecret` | string | | Sent as the `Authorization` header to the Edge Script |
| `ElevenLabsAPIKey` | string | | Used by `elevenLabsClient` |
| `AnthropicAPIKey` | string | | Used by `anthropicClient` |
| `AnthropicModel` | string | `"claude-sonnet-4-6"` | Passed to every Anthropic API call |
| `DefaultCTAURL` | string | | Fallback CTA when no link is found in Telegram posts |
| `OfferDescription` | string | | Injected into Step B as the Bridge offer description |
| `TranscriptionLanguage` | string | `"fr"` | BCP-47 code passed to ElevenLabs |

## Email Pipeline (Steps A–F)

All steps are in `server/anthropic_pipeline.go`. Each step calls `anthropicClient.Send(system, user)` with exponential backoff (3 attempts, 2s/4s delays).

| Step | Function | Input | Output | Retries |
|---|---|---|---|---|
| A | `stepA` | DayContext, CTALinksFound, DefaultCTAURL | CTA brief (URL, text, placement) | None |
| B | `stepB` | CTA brief, OfferDescription, DayContext summary, brand/audience/avatar/nurture/storyselling | Storyselling email draft (FR) | None |
| C | `stepC` | Email draft | Reviewed draft from `<revised_email>`; loops up to 3× until `VERDICT: PASS` | Up to 3 iterations |
| D | `stepD` | Reviewed draft | Humanized draft from `<humanized_email>` (AI artefacts removed) | None |
| E | `stepE` | Humanized draft, CTA brief | Liquid code (6 languages) from `<liquid_email>`; must pass `validateLiquid` | 2 attempts, then the run fails |
| F | `stepF` | Liquid output | Final Liquid from `<final_liquid>`; loops up to 2× until `VERDICT: READY FOR PRODUCTION` | Up to 2 iterations |

Every step's input is wrapped in XML-style tags and every output is read back from tags with `extractTagged`. Only the extracted content moves to the next step or loop iteration; review notes and verdicts never do. `finalVerdict` reads the text after the last `VERDICT:`.

`validateLiquid` requires a `{%case user.language%}` switch, an `endcase`, and parseable Title and Message body blocks. Step F keeps the last valid version: a fluency pass whose output fails validation is discarded and a warning is posted.

`parseLiquidBlocks` extracts the three `### Title`, `### Pre-header`, `### Message body` sections. Headings inside a code fence are treated as email content, so the body can use `##` subheadings. If parsing fails, the entire output is placed in the body.

## Telegram Webhook Flow

1. Neil generates a 32-byte random secret on first activation and stores it under `neil:webhook:secret`.
2. `registerWebhook` calls Telegram's `setWebhook` with the URL `{SiteURL}/plugins/com.onetake.neil/telegram/webhook` and the secret.
3. Telegram sends each channel post as a POST to that URL with the `X-Telegram-Bot-Api-Secret-Token` header.
4. `handleTelegramWebhook` (in `api.go`) validates the header, calls `parseUpdate`, and stores the post + appends to the day index.
5. `registerWebhook` is called again on every `OnConfigurationChange` so a token or URL change automatically re-registers.

The webhook route is registered outside the `MattermostAuthorizationRequired` middleware; Telegram sends unauthenticated requests.

## Media Upload and Transcription

Only posts with `MessageType` of `voice`, `video`, or `video_note` are processed; `text` and `photo` posts are ignored, links included. For each one, `transcribeMediaPost`:

1. Reads the `MediaRecord`. If it already holds a transcription (from an earlier run of the same date), that transcription is reused.
2. If there is no CDN URL yet, `uploadMedia` fetches the file from Telegram (via `GetFileURL` + `DownloadFile`), uploads it to BunnyCDN via the Edge Script, and stores the URL.
3. Sends the CDN URL to ElevenLabs and stores the transcription in the same record. An empty transcription counts as a failure.

A post that fails at any step gets one warning in Mattermost and is left out of the email. `buildDayContext` uses only the successfully transcribed posts, and CTA links come only from their captions. If no post was transcribed, the run stops with status `failed_no_transcription`.

The CDN URL is public. ElevenLabs fetches the file directly from the CDN.

Telegram error messages are passed through `telegramClient.redact` because Go's HTTP errors include the request URL, which embeds the bot token. Telegram entity offsets are UTF-16 code units; `bareURLs` slices accordingly.

## CET Timezone

`cetLocation` is initialized in `processor.go`'s `init()` using `time.LoadLocation("Europe/Paris")`. If the timezone database is absent, it falls back to `time.FixedZone("CET", 3600)` (UTC+1, non-DST). The `_ "time/tzdata"` import in `main.go` embeds the full IANA timezone database into the binary to prevent this fallback under normal conditions.

## Asset Loading

`loadAssets` reads `brand-profile.md`, `audience-segment.md`, and `avatar.md` from `{GetBundlePath()}/assets/` at processor run time. If any file is missing, the pipeline runs with empty strings for those inputs and logs a warning.

## Extending the Pipeline

To add a new pipeline step:
1. Add a `stepG` function in `anthropic_pipeline.go` following the same signature pattern.
2. Call it from `RunPipeline` in the correct sequence.
3. Add the relevant skill content as a constant in `skills.go` if the step needs a new system prompt section.

To change the email language set:
- Update the `skillEmailLiquidOutput` constant in `skills.go`.
- Update the `skillEmailFluencyCheck` constant if the new language needs specific fluency notes.
- No structural code changes are needed.

To change the morning run time:
- The job fires every hour via `cluster.MakeWaitForRoundedInterval(1*time.Hour)`.
- The guard in `runJob` checks `currentHourInCET() != 6`. Change `6` to any 0–23 value.

## Common Maintenance Tasks

**Update the offer description without redeploying:** Change the **Offer Description** setting in System Console. It is injected into Step B at run time.

**Replay a past date:** Run `/neil run 2025-01-15`. The processor accepts any date string in `YYYY-MM-DD` format; an empty string defaults to yesterday.

**Rotate the BunnyCDN secret:** Update `UPLOAD_SECRET` in the Bunny Edge Script dashboard and the **BunnyCDN Edge Script Secret** setting in Mattermost. Changes take effect immediately on the next processor run.

**Rotate the Telegram webhook secret:** Delete the KV key `neil:webhook:secret` via the Mattermost KV store API or a custom admin endpoint. On the next plugin reload, `registerWebhook` generates a new secret and re-registers.
