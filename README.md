# Neil — Daily Storyselling Email Bot

Neil is a Mattermost plugin that monitors a private Telegram channel, transcribes voice and video content posted there, and generates a production-ready storyselling email every morning at 06:00 CET. The output is a six-language Liquid code block ready to paste into UserList.

Neil never touches your inbox or your email tool directly. It posts the finished Liquid code into a Mattermost channel so a human can review and send it.

## How It Works

1. Throughout the day, voice messages, videos, and text posts are sent to a private Telegram channel.
2. Neil's Telegram webhook receives each post and stores it in the Mattermost KV store.
3. At 06:00 CET, Neil automatically runs the morning processor:
   - Checks that at least one audio or video post was received.
   - Has a Bunny Edge Script fetch each audio/video file straight from Telegram into BunnyCDN storage, so files never pass through the Mattermost server (retried up to 3 times on temporary errors).
   - Transcribes each audio/video file using ElevenLabs Scribe v2.
   - Builds the email only from the audio/video posts that were transcribed successfully. Text and photo posts are ignored, including their links; CTA links come from the audio/video captions or the **Default CTA URL**.
   - Runs a six-step Anthropic Claude pipeline to produce the email.
   - Posts the finished Liquid code to the configured Mattermost channel.

## Prerequisites

- Mattermost 6.2.1 or later
- A Telegram bot with access to channel posts (BotFather token)
- A BunnyCDN account with a Storage Zone and a Pull Zone
- An ElevenLabs account with API access (Scribe v2)
- An Anthropic API key

## Build

```bash
make dist
```

This produces `dist/com.onetake.neil-{version}.tar.gz`. Go 1.22+ and Node 16+ are required.

## Install

1. In Mattermost, go to **System Console > Plugin Management**.
2. Upload the `.tar.gz` bundle.
3. Enable the plugin.

## Initial Setup

### 1. Create a Telegram Bot

1. Message `@BotFather` on Telegram and run `/newbot`.
2. Copy the bot token — you will paste it into the **Telegram Bot Token** setting.
3. Add the bot to your private Telegram channel as an administrator with "Read Messages" permission.

### 2. Deploy the Bunny Edge Script

The Edge Script (`edge-scripts/neil-upload.js`) downloads each media file from Telegram and stores it on BunnyCDN. It keeps your BunnyCDN API key server-side, and it only accepts files from `api.telegram.org`.

1. In the Bunny dashboard, go to **Edge Scripting** and create a new script. Its ID (currently `87710`) is set in `.github/workflows/deploy-bunny-edge-script.yml`.
2. Connect this GitHub repository to Bunny (**Settings → Connected Repositories**). The workflow deploys `edge-scripts/neil-upload.js` on every push to `master` that changes it; you can also run it by hand from the **Actions** tab (**Deploy Neil Edge Script to Bunny → Run workflow**).
3. Set the following environment variables in the script settings:

| Variable | Description |
|---|---|
| `UPLOAD_SECRET` | A secret string you invent; paste the same value into the **BunnyCDN Edge Script Secret** plugin setting |
| `STORAGE_ZONE` | Your BunnyCDN storage zone name |
| `STORAGE_API_KEY` | The storage zone password (FTP & API Access tab) |
| `STORAGE_HOSTNAME` | e.g. `storage.bunnycdn.com` for EU; `de.storage.bunnycdn.com` for Germany |
| `CDN_HOSTNAME` | Your pull zone hostname, e.g. `assets.example.b-cdn.net` |

4. Note the Edge Script's URL (currently `https://neil-mattermost-marketing-bot-2kddw.bunny.run`) and paste it into the **BunnyCDN Edge Script URL** plugin setting.

### 3. Configure the Plugin

Go to **System Console > Plugins > Neil** and fill in all settings.

| Setting | Description |
|---|---|
| **Bot Display Name** | The name shown on Neil's Mattermost posts. Default: `Neil`. |
| **Channel ID** | The Mattermost channel where Neil posts results. Find it in the channel URL. |
| **Users to Tag** | Comma-separated usernames to @-mention in each post (e.g. `seb,julie`). These users can also run `/neil run`. |
| **Telegram Channel Username** | The @username of the Telegram channel to monitor, without the `@`. |
| **Telegram Bot Token** | The token from BotFather. |
| **BunnyCDN Edge Script URL** | The base URL of the deployed Edge Script, e.g. `https://neil-upload.b-cdn.net`. |
| **BunnyCDN Edge Script Secret** | The shared secret you set as `UPLOAD_SECRET` in the Edge Script. |
| **ElevenLabs API Key** | Your ElevenLabs API key for Scribe v2 transcription. |
| **Anthropic API Key** | Your Anthropic API key for Claude. |
| **Anthropic Model** | The Claude model string. Default: `claude-sonnet-4-6`. |
| **Default CTA URL** | The fallback link used when no URL is found in the day's Telegram posts. |
| **Offer Description** | A short description of the current OneTake offer. Injected into the email's Bridge step. Update this whenever the offer or promotion changes — no redeployment needed. |
| **Transcription Language Code** | BCP-47 language code for ElevenLabs transcription. Default: `fr` (French). |

After saving, Neil automatically registers the Telegram webhook with your bot token and site URL.

## Triggering the Processor

The processor runs automatically at 06:00 CET every day.

To run it manually at any time:

```
/neil run
```

To run it for a specific past date:

```
/neil run 2025-01-15
```

You can also trigger it by posting `@neil run` in any Mattermost channel.

A date that isn't in `YYYY-MM-DD` format is refused rather than replaced by yesterday. Only one run per date can be in progress at a time.

To see where the last run for a date got to (without a date, yesterday):

```
/neil status 2026-10-21
```

It shows the last step reached, when it was reached, and any error. A run marked "interrupted" stopped without finishing, usually because the plugin restarted (for example after uploading a new build); run it again.

Users listed in **Users to Tag** and Mattermost system admins can run these commands.

## What Neil Posts

As soon as a run starts, Neil posts a progress message and keeps editing it: one line per step, with the time it started (CET). When the run ends, the first line shows the outcome. If a run goes quiet, this message shows where it stopped.

While the run goes on, Neil replies in that message's thread with each intermediate result, so you can check it is heading in the right direction: each transcription (marked when it was reused from an earlier run), the call to action brief (step A), the first draft (B), the reviewed draft (C), the humanized draft (D) and the six-language Liquid before the fluency check (E). Each result is attached as a `.txt` file, and also shown in the message when it fits. An empty result is marked `(empty)`.

Replaying a date doesn't fetch or transcribe the same audio again: the Bunny URL and transcription of each message are stored and reused. Only the email steps (A to F) run again.

A run is stopped after 45 minutes. Neil then posts which step it was on; replaying the date reuses the transcriptions already done.

When the pipeline completes, Neil posts three Liquid code blocks to the configured channel. Mattermost limits a message to 16,383 characters, so when the six-language email is longer, the message body is attached as `neil-YYYY-MM-DD-message-body.txt` and the title and pre-header stay in the message:

- **Title** — the email subject line in six languages
- **Pre-header** — the inbox preview text in six languages
- **Message body** — the full email in six languages, including UTM-tagged links and the signature snippet

If no audio or video content was found for the day, Neil posts a warning and stops.

If an audio or video post can't be uploaded or transcribed, Neil posts one warning for that message and writes the email from the other posts. If none of the day's audio or video posts could be transcribed, Neil posts a warning and generates no email.

If a pipeline step fails, Neil posts a warning with the error details and the step that failed. An unexpected error in Neil's code is caught and reported the same way instead of crashing the plugin.

## Troubleshooting

**No webhook activity from Telegram.** Make sure:
- The bot is added to the channel as an administrator.
- The Mattermost site URL is publicly reachable by Telegram's servers.
- The **Telegram Bot Token** setting is saved; Neil registers the webhook automatically on each configuration save.

**`Media upload failed … status 502 … We could not establish a connection to ….bunny.run`.** Bunny's edge could not reach the Edge Script, so the script is not running. Neil already retried 3 times. Open the **BunnyCDN Edge Script URL** in a browser: a healthy script answers `{"error":"Method not allowed"}`, while the Bunny 502 page confirms the script is down. In the Bunny dashboard, check that the latest version of `edge-scripts/neil-upload.js` is published and look at the script's logs for a startup error. Then replay the date with `/neil run YYYY-MM-DD`; files already uploaded or transcribed are reused.

**Transcription returns empty text.** Verify that the media was uploaded to BunnyCDN successfully (check the Mattermost logs for `Neil: media upload failed`). ElevenLabs Scribe v2 requires a publicly accessible URL.

**Pipeline error at a specific step.** The Mattermost logs contain detailed error messages tagged `Neil:`. Each pipeline step error includes the step name (e.g. `step B (write email)`).

**Already ran today, but you want to re-run.** Use `/neil run 2025-01-15` with the explicit date. The job's duplicate-run guard checks the stored run status; the manual command bypasses it.
