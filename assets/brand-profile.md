# Brand Profile — OneTake AI

## Sender

**Company:** OneTake Pte Ltd (Singapore)
**Product:** OneTake
**Sender name:** Sébastien Night
**Sender email:** sebastien@team.onetake.ai
**Platform syntax for first name:** `{{user.first_name | capitalize}}`

## What OneTake Is

OneTake is an AI agent that turns raw video footage into professional, profitable content instantly. It handles the entire editing process automatically: removes background noise and awkward silences, adds captions and highlights key points on screen, creates transitions when switching topics, composes a unique background track tailored to the video, and translates the video into the viewer's language.

OneTake lets users edit and modify their videos by interacting just as they would with a talented videographer.

OneTake is for expert entrepreneurs — coaches, consultants, and online trainers — who want to build their online presence through video content without spending a fortune on equipment, editing time, or a production team.

## Ideal Client

An "expert" entrepreneur: a coach, consultant, or in-person or online trainer who wants to build their online presence through video content. They are non-technical users, working either solo or with a small team. Their current products and services consist of training in all formats (print and e-books, in-person or online conferences, etc.), and they want to transition to selling video training products — or sell more of them — to automate sales and gain freedom.

## Voice and Tone

- **Personal and direct:** One person writing to one specific person. Never "Hi everyone" — always "Bonjour {{user.first_name | capitalize}},"
- **Confident, never aggressive:** Sébastien writes like a mentor who has been there, done that. He shares what worked for him, naturally bridging to what he is selling. He never pitches.
- **Authentic:** Willing to be vulnerable, honest, and specific. Avoids empty claims and always shows rather than tells.
- **Informal:** Always uses "tu" in French, "tú" in Spanish, "você" informal register in Brazilian Portuguese, "tu" in Italian, casual register in Japanese.
- **Warm but not gushing:** No "Dear valued customers," no "We hope this email finds you well." Jump straight into the content.
- **Storytelling-first:** Every email begins with a story, not a pitch. OneTake enters naturally through the Bridge.

## Communication Rules

| Never write | Write instead |
|---|---|
| "OneTake is a tool/platform/software" | "OneTake is an AI agent" |
| "The AI removes silences" | "OneTake removes silences" |
| "Founders will benefit from..." | "Entrepreneurs like you will benefit from..." |
| "Build your brand" | "Grow your business and reach more customers" |
| "To be honest..." / "The truth is..." | Simply state the fact directly |
| "API integration capabilities" | "OneTake connects with the apps you already use" |
| "Leverage our cutting-edge technology" | "Use OneTake to save time and create better videos" |
| "Dear valued customers" | "Hello" + use their actual name |
| "Your niche" | "Your domain or industry" |
| "We hope this email finds you well" | Jump straight into the content |
| (in French) "éditer une vidéo" | Monter une vidéo |

## UTM Defaults

- `utm_source`: `{{user.utm_source}}`
- `utm_medium`: `email`
- `utm_campaign`: kebab-case slug for the campaign (e.g. `daily-nurture`)
- `utm_content`: kebab-case slug for this specific email (e.g. `story-dubai-workshop`)
