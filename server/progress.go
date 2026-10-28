package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
)

// runProgress keeps a single Mattermost post up to date with the steps of a run, and
// mirrors the current step into the run status so `/neil status` can show it.
type runProgress struct {
	p      *Plugin
	date   string
	status *RunStatus
	post   *model.Post
	state  string
	lines  []string
}

func (p *Plugin) newRunProgress(date string, status *RunStatus) *runProgress {
	return &runProgress{p: p, date: date, status: status, state: "⏳ in progress"}
}

// step records that the run has moved on to a new step.
func (r *runProgress) step(name string) {
	r.p.API.LogInfo("Neil: "+name, "date", r.date)

	r.status.Step = name
	r.status.UpdatedAt = time.Now()
	if err := r.p.kvstore.StoreRunStatus(r.date, r.status); err != nil {
		r.p.API.LogWarn("Neil: failed to store run status", "error", err.Error())
	}

	r.lines = append(r.lines, fmt.Sprintf("- %s %s", time.Now().In(cetLocation).Format("15:04"), name))
	r.render()
}

// result posts a step's output as a reply in the run's thread, so its direction can be
// checked while the run goes on. The output is always attached as a .txt file.
func (r *runProgress) result(slug, title, text string, liquid bool) {
	rootID := ""
	if r.post != nil {
		rootID = r.post.Id
	}
	filename := fmt.Sprintf("neil-%s-%s.txt", r.date, slug)
	if err := r.p.postText(rootID, "**"+title+"**", text, liquid, filename); err != nil {
		r.p.API.LogWarn("Neil: failed to post a step result", "step", title, "error", err.Error())
	}
}

// finish marks the run as over. The outcome itself is posted as a separate message.
func (r *runProgress) finish(state string) {
	r.state = state
	r.render()
}

func (r *runProgress) render() {
	msg := fmt.Sprintf("**Neil run for %s**: %s\n%s", r.date, r.state, strings.Join(r.lines, "\n"))
	if r.post == nil {
		r.post = r.p.createChannelPost(msg)
		return
	}
	r.post.Message = msg
	updated, appErr := r.p.API.UpdatePost(r.post)
	if appErr != nil {
		r.p.API.LogWarn("Neil: failed to update progress post", "error", appErr.Error())
		return
	}
	r.post = updated
}

// RunStatusReport describes where the last run for date got to, for `/neil status`.
// An empty date means yesterday in CET.
func (p *Plugin) RunStatusReport(date string) string {
	if date == "" {
		date = yesterdayInCET()
	}

	var status RunStatus
	if err := p.kvstore.GetRunStatus(date, &status); err != nil {
		return fmt.Sprintf("Could not read the run status for %s: %s", date, err.Error())
	}
	if status.Status == "" {
		return fmt.Sprintf("Neil has not run for %s yet. Start it with `/neil run %s`.", date, date)
	}

	_, running := p.runningDates.Load(date)
	return formatRunStatus(date, status, running, time.Now())
}

// formatRunStatus renders a run status. running tells whether a run for the date is
// active in this plugin process right now.
func formatRunStatus(date string, status RunStatus, running bool, now time.Time) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "**Neil run for %s**: %s\n", date, describeRunState(date, status.Status, running))
	fmt.Fprintf(&sb, "- Started: %s\n", status.StartedAt.In(cetLocation).Format("Jan 2, 15:04 MST"))
	fmt.Fprintf(&sb, "- Last update: %s (%s ago)\n",
		status.UpdatedAt.In(cetLocation).Format("Jan 2, 15:04 MST"),
		now.Sub(status.UpdatedAt).Round(time.Minute))
	if status.Step != "" {
		fmt.Fprintf(&sb, "- Last step: %s\n", status.Step)
	}
	if status.Error != "" {
		fmt.Fprintf(&sb, "- Error: %s\n", status.Error)
	}
	for _, w := range status.Warnings {
		fmt.Fprintf(&sb, "- %s\n", w)
	}
	return strings.TrimSuffix(sb.String(), "\n")
}

func describeRunState(date, status string, running bool) string {
	switch status {
	case "started":
		if running {
			return "⏳ in progress"
		}
		// The status says started but no run is active: the plugin restarted mid-run.
		return fmt.Sprintf("⚠️ interrupted before it finished, most likely because the plugin restarted. Run `/neil run %s` to retry.", date)
	case "completed":
		return "✅ completed; the email was posted"
	case "completed_no_content":
		return "⚪ completed; there were no audio or video posts"
	case "failed_no_transcription":
		return "❌ failed; no audio or video post could be transcribed"
	case "failed_timeout":
		return fmt.Sprintf("⏱ stopped after %d minutes", int(processorTimeout.Minutes()))
	case "failed":
		return "❌ failed"
	default:
		return status
	}
}
