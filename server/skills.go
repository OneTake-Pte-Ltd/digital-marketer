package main

// skillEmailPrinciples contains the email marketing rules that govern every pipeline step.
const skillEmailPrinciples = `
# Email Marketing Principles

These are the non-negotiable rules that govern every email written across all campaign skills. Every principle here comes directly from Sébastien Night's email marketing training. When in doubt, come back to this file.

## 1. Opt-in Only

These skills are exclusively for opt-in email marketing. The recipient must have explicitly requested to hear from the sender.

## 2. One Person Writing to One Person

Every email must read as if one specific human being sat down and typed it to one specific reader. This is the most important stylistic rule.

Always write:
- "Bonjour {{user.first_name | capitalize}}," (using the platform's exact syntax)
- From a named individual, not a brand or team
- In a personal, direct register — as if writing to a friend

When drafting, mentally address the email to one real, specific person who matches the audience description.

## 3. Never Send a Single Email

A lone email is a wasted email. Every campaign must contain at least two emails with the same goal and the same call to action.

## 4. One Call to Action Per Email

Every email has exactly one destination — one thing the reader should do next. The link to that destination can (and should) appear multiple times in the email, but it must always be the same link.

Never include two different links in the same email. Exception: multiple links are acceptable if they all lead to the same content in different formats (e.g. YouTube + podcast version of the same episode).

The CTA destination must always be a sales page, registration page, or content page — never a direct payment link.

## 5. CTA Placement

The call to action link must appear:
- Near the top of the email — within the first 2–3 sentences or short paragraph, after the opening hook
- Near the bottom of the email — as a closing link or sign-off
- Inline throughout longer emails — whenever the benefit being described is directly related to the CTA

## 6. The Goal of the Email is the Click — Not the Sale

The email does not sell. The landing page sells. The email has one job: get the reader curious and motivated enough to click. Do not try to explain the full offer, list all features, justify the price, or address every objection inside the email.

## 7. Subject Line Formula

Every subject line must contain two elements:
- Benefit: What is in it for the reader? Make it explicit.
- Curiosity: Do not give away the answer in the subject line.

Additional rules:
- Keep it short — most important words must come first
- Front-load: "Important thing: detail" not "Here is a detail about the important thing"
- Avoid generic AI-written phrases: "revolutionary", "game-changing", "unlock your potential"
- Avoid subject lines about the sender's company or product features

## 8. Sender Consistency

The sender name shown in the "From" field must be a single, real, named person. Recognizable — readers must immediately know who sent this.

## 9. Segmentation

Never send an email to the entire list if it is not relevant to everyone on it. An email that is irrelevant to 70% of recipients trains the audience to stop opening emails.

## 10. Email Frequency

Send as often as relevance allows. A 15% daily open rate outperforms a 40% weekly open rate in cumulative reach, clicks, and revenue.

## 11. Open Rates Are Not a Reliable Metric

The only metrics that matter are: Clicks, Replies, Revenue generated per email.

## 12. Deliverability Basics

- Emails must be sent from a custom domain — never from Gmail, Hotmail, Yahoo, or free providers
- In welcome emails, ask the reader to reply or click as soon as possible

## 14. The Five Persuasion Levers (Blair Warren)

Every email must activate at least one of these five levers:

1. Encourage their dreams — Affirm that what the reader wants is possible
2. Justify their failures — Name the real reason past attempts didn't work (not their fault)
3. Allay their fears — Directly address what scares them about the next step
4. Confirm their suspicions — Say out loud what they already feel but haven't heard validated
5. Help them throw rocks at their enemies — Give them a shared villain to reject together
`

// skillCallToAction defines how to build and validate the email CTA.
const skillCallToAction = `
# Call to Action Skill

The CTA is decided before the email is written — not discovered at the end. Every word of the email body exists to build momentum toward this one action.

## Step 1 — Validate the Destination

The destination URL must be a page that can complete the job the email started:

| Email goal | Valid destination | Invalid destination |
|---|---|---|
| Sell a product | Sales page / offer page | Direct payment link |
| Register for webinar | Registration page | Zoom link directly |
| Nurture / educate | Blog post, video, podcast | Unrelated product page |

## Step 2 — Define the CTA Text

Effective CTA text patterns:
- "Click here to save your seat" (click here + benefit)
- "Click here to watch the video"
- "Yes, I want free access"
- Imperative form: "Save my seat", "Regarde la vidéo", "Rejoins le Cercle"

Avoid: "Learn more", "Find out", "Visit our website", passive constructions.

## Step 3 — Define CTA Placement

Every email must include the CTA link in at least two positions:
1. Near the top — within the first 2–3 sentences after the opening hook
2. Near the bottom — as a closing statement or sign-off link

For emails longer than ~150 words, also add the link inline whenever the email mentions the benefit.

## Step 4 — Output a CTA Brief

Output:
- Goal (what business outcome does this email support?)
- Primary CTA destination URL
- CTA text (primary and alt)
- Placement: top, inline (if applicable), bottom
- Whether to use the provided CTA URL or the default CTA URL

## Validation Checklist

- Primary destination is a content or sales page — not a direct payment link
- Primary CTA text is action-oriented and specific
- Primary CTA appears near the top
- Primary CTA appears near the bottom
- Primary CTA is consistent with the subject line's promise
`

// skillCampaignNurture defines the format for storyselling nurture emails.
const skillCampaignNurture = `
# Campaign: Nurture / Regular Newsletter

Nurture emails are the ongoing heartbeat of the email program. They are regular, human, relevant emails that keep the sender present and trusted in the reader's inbox.

Done well, nurture emails do three things:
1. Warm the list — readers who open and click nurture emails are far more likely to buy during a launch
2. Build authority — consistent, useful content establishes the sender as the go-to expert
3. Generate everyday revenue — nurture emails include a relevant CTA

## Format: Personal Story (Storyselling)

For this daily email, use the Personal Story format.

Structure:
- Subject line: emotional, first-person — specific and honest
- Opening: drop the reader into the moment — specific time, place, situation
- The story: honest, with a clear turning point. Keep it tight: the whole body (hook to signature, without the P.S.) is 250 to 350 words.
- The lesson and bridge: what did the sender learn, and how does it connect to OneTake?
- CTA: link to related content or product — natural, not forced
- Images: if photos are available to illustrate the story, include them

## Frequency and Segmentation

This email goes out daily. Segmentation is what makes higher frequency sustainable — every email must be relevant to the segment.

## Nurture Email Checklist

Before finalizing:
- Subject line passes benefit + curiosity test
- Email is written to one person, from one person
- Content matches the segment's topic contract
- CTA destination is a content page or product page — not a direct payment link
- Primary CTA appears near the top and near the bottom
- Email does not try to make the sale — the landing page does
`

// skillEmailCopyReview is the 14-point review checklist applied to every email draft.
const skillEmailCopyReview = `
# Email Copy Review

This skill runs a systematic check of completed email drafts. Every issue flagged must include a suggested rewrite — a flag without a fix is not a review.

## Check 1 — Subject Line
- Does the subject line contain both a clear benefit AND a curiosity gap?
- Is it front-loaded — most important word in the first 3 words?
- Is it safe at 40 characters (mobile truncation)?

## Check 2 — Structure: One Sentence, One Paragraph, One Page
- First sentence must contain the most important idea
- Each paragraph contains one distinct idea only
- Sentences are short — flag any sentence with more than one "et/and"

## Check 3 — Parentheses
- Are parentheses avoided or minimized?
- No full paragraphs inside parentheses

## Check 4 — Bullet Lists
- Bullet lists use proper list markup, not manual dashes
- Each bullet is a complete, standalone idea

## Check 5 — Specificity
Replace: quelque chose, une chose, des choses, bon, mauvais, meilleur, pire.
Every vague word must become specific.

## Check 6 — New Concepts
Every term a non-specialist would not understand needs a definition.

## Check 7 — Real People
When a real named person is mentioned: at least one character trait AND one physical trait.

## Check 8 — Inclusive Writing (French only)
No mid-word punctuation (seul.e, entrepreneur.e). Use epicene rewriting instead.

## Check 9 — Banned Expressions
- "augmentez vos revenus" / "increase your income"
- "gagnez plus d'argent" / "earn more money"
- "d'ailleurs" (filler — always cut or rewrite)

## Check 10 — First-Name Tag
- Maximum one use per email
- Always precede with a salutation: "Bonjour {{user.first_name | capitalize}},"

## Check 11 — "Si vous" Conditionals
"Si vous voulez [résultat], cliquez ici" — always a violation. Rewrite as a direct statement of benefit.
Exception: a qualifying "if" at the very opening to help readers self-select is allowed.

## Check 12 — Links and CTAs
- No more than one primary CTA destination
- Every link must carry UTM parameters (note as a UTM reminder, not a blocking issue at this stage)
- Every image has a centered caption and an ALT tag

## Check 13 — Deadline and Urgency Integrity
No "I changed my mind" emails that extend previously announced deadlines.

## Check 14 — Sequence Continuity (for sequences)
- Each email references the previous one naturally
- Each email (except the last) ends with a forward reference to the next

## Verdict Format

For each email:
PASS — no issues found.
OR
NEEDS REVISION — list issues with specific rewrites.
`

// skillHumanizeCopy removes AI writing artefacts from completed copy.
const skillHumanizeCopy = `
# Humanize Copy

AI-generated text carries recognizable fingerprints. This skill systematically identifies and removes those fingerprints. The substance, argument, and sequence of the copy do not change. Only the artefacts are removed.

## Category 1 — Word Choice

Magic adverbs — "quietly", "deeply", "fundamentally", "remarkably", "arguably". Cut entirely.

Overused AI vocabulary — eliminate: delve/delving, certainly, utilize (use "use"), leverage (as a verb), robust, streamline, harness, tapestry, landscape (when describing a domain), paradigm, synergy, ecosystem (metaphorical), framework (vague).

"Serves as" dodges — replace "serves as", "stands as", "marks", "represents" with the simpler "is".

## Category 2 — Sentence Structure

Negative parallelism — "It's not X — it's Y." One instance per piece maximum. Rewrite as direct positive statement.

"Not X. Not Y. Just Z." — state Z directly.

Self-posed rhetorical questions — "The result? Devastating." Merge into a single direct sentence.

Filler transition phrases — cut: "It's worth noting that", "It bears mentioning", "Importantly,", "Interestingly,", "Notably,"

Superficial "-ing" analysis — cut phrases like "highlighting its importance", "reflecting broader trends".

## Category 3 — Paragraph Structure

Short punchy fragments as standalone paragraphs — "Platforms do." / "Every time." Merge into surrounding text.

## Category 4 — Tone

"Here's the kicker" / false suspense — cut: "Here's the kicker.", "Here's the thing.", "Here's where it gets interesting."

Patronizing analogies — "Think of it as..." / "It's like a..." when not needed.

"Let's break this down" / pedagogical hand-holding — cut: "Let's break this down.", "Let's unpack this.", "Let's explore.", "Let's dive in."

## Category 5 — Formatting

Em-dash use — rewrite every em-dash as separate sentences or use parentheses, commas, ellipses, colons, or semicolons. Zero em-dashes allowed.

Bold-first bullets — use bold only when a specific term genuinely needs to stand out.

## Category 6 — Composition

Fractal summaries — cut all "In this section we will..." and "As we've seen..." announcements.

The signposted conclusion — cut "In conclusion...", "To sum up...", "In summary..."

## Category 7: Active Voice and Full Sentences

Passive voice and participle lists: name who acts and use an active verb. Not "silences enlevés, titres ajoutés" but "OneTake enlève les silences et ajoute les titres". Not "the video was edited in minutes" but "OneTake edited the video in minutes".

Nominal phrases (sentences without a verb): write natural sentences with a subject and a verb. Not "A video to shoot. Five minutes left." but "You've got a video to shoot and only five minutes to do it." Not "Une vidéo à tourner. Cinq minutes devant toi." but "Tu as une vidéo à tourner et seulement cinq minutes pour le faire."

## Category 8: Length

Never make the copy longer. If the body (hook to signature, without the P.S.) is over 350 words, cut it to 250 to 350 words: remove repetitions, secondary details and extra examples, never the turning point of the story or the CTA.

## Category 9: Placeholders

The email goes out as written. Remove every placeholder, bracketed note or instruction to the sender (for example "[Trait physique à confirmer par Sébastien avant l'envoi.]"). Rewrite the sentence so it reads naturally without the missing detail. Never invent a detail to fill the gap.

## Verdict Format

Output the full revised copy in one clean block — no tracked changes, no annotations.
`

// skillEmailLiquidOutput converts reviewed copy into production-ready Liquid code.
const skillEmailLiquidOutput = `
# Email Liquid Output

This skill takes a completed, humanized, reviewed email and produces the final Liquid code block ready for UserList.

## Deliverables

For every email, produce Liquid code for three elements:
1. Title — short, energetic, benefit-driven
2. Pre-header — inbox preview line; must have shock value and incite curiosity
3. Message body — full email content

All three in six languages: French, Spanish, Brazilian Portuguese, Italian, Japanese, English.

## Language Structure

Use a case switch — never a series of if statements:

` + "```liquid" + `
{%case user.language%}
{% when 'fr' %}
CONTENU FRANÇAIS
{% when 'es' %}
CONTENIDO EN ESPAÑOL
{% when 'pt' %}
CONTEÚDO EM PORTUGUÊS DO BRAZIL
{% when 'it' %}
CONTENUTO IN ITALIANO
{% when 'ja' %}
日本語コンテンツ
{% else %}
ENGLISH CONTENT
{% endcase %}
` + "```" + `

The {% else %} branch always contains the English version.

## Signature

If there is no P.S.: place ` + "`{{snippets.en_signature_sebastien}}`" + ` at the very end of the message body, after {% endcase %}. The snippet appears only once.

If there is a P.S.: place the signature snippet before the P.S. in each language version (so the snippet appears once inside each language).

## Text Formatting

- Write message content in Markdown
- Bold is available: **bold text**
- H2 headings: ## Heading
- Single carriage returns only — never empty lines between paragraphs
- Remove all double carriage returns from the source draft

## Tone

Informal and friendly in all languages:
- French: tu (never vous)
- Spanish: tú (never usted)
- Portuguese: Brazilian você informal register (never o senhor/a senhora)
- Italian: tu (never Lei)
- Japanese: casual register appropriate for a trusted peer
- English: direct "you"

## Link Formatting

Primary CTA: Markdown link on its own line with no period at the end. Anchor text 3–6 words max. At least one CTA prefixed with "Click here to..." (French: "Clique ici pour...").

UTM parameters on every link:
- utm_source: ` + "`{{user.utm_source}}`" + ` (always this Liquid variable — never hardcoded)
- utm_medium: email
- utm_campaign: kebab-case slug for the campaign (e.g. daily-nurture)
- utm_content: kebab-case slug for this specific email

## Localizing Time

Whenever a specific time is referenced:
- French: Paris time + Québec/Guadeloupe/Martinique time
- Spanish: Eastern Time + Madrid time
- Portuguese: Eastern Time + Brasília time
- Italian: Eastern Time + Roma time
- Japanese: Eastern Time + Tokyo time
- English: Eastern Time + Pacific Time

## Localizing Currency

- French: Euros and Canadian dollars (1€ = 1.6 CAD)
- Spanish: Euros only
- Portuguese: Euros + Brazilian Real (1€ = 6 BRL)
- Italian: Euros only
- Japanese: Euros + Japanese Yen (1€ = 185 JPY)
- English: Euros + US Dollars (1€ = 1.15 USD)

## Output Format

Three separate code blocks:
- Title: the whole case switch on one single line, with no line breaks, e.g. {%case user.language%}{% when 'fr' %}Titre{% when 'es' %}Título{% when 'pt' %}Título{% when 'it' %}Titolo{% when 'ja' %}タイトル{% else %}Title{% endcase %}
- Pre-header: the whole case switch on one single line, same shape as the title
- Message body (multi-line case switch, with {{snippets.en_signature_sebastien}})
`

// skillEmailFluencyCheck reviews non-English language versions for native naturalness.
const skillEmailFluencyCheck = `
# Email Fluency Check

This skill catches translation artefacts. It does not check structure, links, or Liquid syntax. It checks one thing: does each language version sound like it was written by a native speaker, for a native reader?

## The Standard

The bar is not grammatical correctness. It is native naturalness. A sentence can be grammatically impeccable and still read as translated.

## What to Look For

Calques and literal translations — phrases translated word-for-word that do not exist naturally in the target language.

Anglicisms where native equivalents exist — borrowing English words when the target language has a perfectly natural native equivalent.

Sentence rhythm mismatches — each language has its own natural sentence length and clause structure. Flag sentences that carry the rhythm of the source language rather than the target.

Register drift — the tu/tú/tu informal register must hold throughout. Flag any slip into vous, usted, Lei, or formal Japanese keigo.

Idioms that do not cross languages — idiomatic expressions translated literally instead of replaced with a target-language equivalent.

## Per-Language Focus Areas

French — English loanwords used over French equivalents; formal register leaking in.

Spanish — calques from English business idioms; tú vs usted inconsistency; missing inverted punctuation (¡, ¿).

Portuguese — European Portuguese constructions in what should be Brazilian Portuguese (e.g. a fazer vs fazendo; óptimo vs ótimo); overly formal register.

Italian — false cognates borrowed from French or English; loss of natural Italian cadence.

Japanese — sentences following English subject-verb-object order rather than Japanese subject-object-verb; formal keigo where warm and casual is needed.

## Verdict Format

For each language version:
PASS — reads as natively written.
OR
NEEDS REVISION — list issues with specific rewrites.

Overall verdict: READY FOR PRODUCTION / REVISIONS REQUIRED
`

// storysellingFramework is injected into the Step B system prompt.
const storysellingFramework = `
## Storyselling Email Framework

The email must follow this exact five-step structure:

1. **Hook** (first 2 lines): A pattern interrupt that creates an open loop. Drop the reader mid-action or mid-thought. No greeting, no context-setting. Create an information gap that can only be closed by reading further.

2. **Setup** (2-4 sentences): Establish context and relatability. When and where the story happened, what the situation was, what was at stake. The reader should think "that's me" or "I've been there."

3. **Tension** (the core): Something goes wrong, something unexpected happens, or something is at risk. Show the consequences viscerally — do not just state the problem. This maps to a pain the reader recognizes.

4. **Bridge**: The insight or turning point where the solution enters the narrative naturally. Three beats:
   - Insight: what the sender realized
   - Category: the type of solution
   - Product: OneTake, introduced through the Offer Description
   The bridge must feel like sharing what worked, not pitching.

5. **CTA**: The natural next step that the story has been building toward. Soft and confident, matching the emotional temperature of the email. Uses the CTA link from the brief.

Build the email backward from the CTA — wrap the selling in the story's logic.

Apply the Blair Warren persuasion levers where they fit naturally within the story structure.
`
