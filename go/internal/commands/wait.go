package commands

import (
	"fmt"
	"time"

	"github.com/grubless/grubless-cli/go/internal/api"
	"github.com/grubless/grubless-cli/go/internal/client"
	"github.com/grubless/grubless-cli/go/internal/jsonv"
	"github.com/grubless/grubless-cli/go/internal/jsstr"
	"github.com/grubless/grubless-cli/go/internal/output"
)

// Waiting for queued work by polling /entities/:id/activity — there is no
// SSE or websocket. A full sync can legitimately take ~30 minutes, hence the
// 35-minute default; and we re-poll the observation, never re-POST the
// action. See src/commands/wait.ts.

// PollInterval is a variable only so tests can shorten it.
var PollInterval = 2 * time.Second

const DefaultWaitTimeoutMs = 35 * 60 * 1000.0

var terminal = map[string]bool{"success": true, "degraded": true, "error": true}

// activityRow is one activity entry in both views: typed for decisions,
// ordered for `--json`.
type activityRow struct {
	api.EntityActivity
	value jsonv.Value
}

type waitResult struct {
	activity []activityRow
	failed   []activityRow
	degraded []activityRow
}

// values is the rows as JSON, for a result's `activity` field.
func (r waitResult) values() []jsonv.Value {
	out := make([]jsonv.Value, len(r.activity))
	for i, a := range r.activity {
		out[i] = a.value
	}
	return out
}

// nowMs is `new Date()` — millisecond precision, as the comparisons in the
// TS are, so a job stamped in the same millisecond compares the same way.
func nowMs() time.Time { return time.Now().Truncate(time.Millisecond) }

// waitForActivity blocks until every activity row started at or after
// `since` has reached a terminal state. `since` is captured by the caller
// BEFORE triggering the job, so one that finishes before the first poll is
// still observed.
func waitForActivity(c *client.Client, entityID string, since time.Time, timeoutMs float64, quiet bool) (waitResult, error) {
	start := nowMs()
	deadline := start.Add(time.Duration(timeoutMs * float64(time.Millisecond)))
	lastMessage := ""
	floor := since.Add(-time.Second)

	for {
		all, value, err := fetch[[]api.EntityActivity](c, "/entities/"+entityID+"/activity")
		if err != nil {
			return waitResult{}, err
		}
		values := items(value)

		var relevant, running []activityRow
		for i, a := range all {
			started, ok := jsstr.ParseDate(a.StartedAt)
			// An unparseable timestamp is NaN in the TS, and NaN >= x is false.
			if !ok || started.Before(floor) {
				continue
			}
			row := activityRow{a, values[i]}
			relevant = append(relevant, row)
			if !terminal[a.Status] {
				running = append(running, row)
			}
		}

		if !quiet && len(running) > 0 {
			// The worker's own progress text ("Fetching page 12…") is the
			// only signal there is; without it the command looks hung.
			message := running[0].JobType
			if running[0].Message != nil {
				message = *running[0].Message
			}
			if message != lastMessage {
				output.Note(output.Dim("  " + message))
				lastMessage = message
			}
		}

		if len(relevant) > 0 && len(running) == 0 {
			r := waitResult{activity: relevant}
			for _, a := range relevant {
				switch a.Status {
				case "error":
					r.failed = append(r.failed, a)
				case "degraded":
					r.degraded = append(r.degraded, a)
				}
			}
			return r, nil
		}

		if nowMs().After(deadline) {
			return waitResult{}, output.Errorf(output.Failure,
				"Timed out after %s minutes waiting for the job to finish.\n"+
					"The job is still running server-side — this only stopped watching it.\n"+
					"Check progress in the web app, or re-run with --timeout <minutes>.",
				jsstr.Number(jsstr.Round(timeoutMs/60000)))
		}
		time.Sleep(PollInterval)
	}
}

// label is `a.source?.label ?? a.jobType`.
func (a activityRow) label() string {
	if a.Source != nil && a.Source.Label != nil {
		return *a.Source.Label
	}
	return a.JobType
}

// reportWaitResult turns a wait into an exit code and summary lines. A
// degraded run is reported but doesn't fail: it finished with a caveat, and
// failing would make a nightly job flap. An error does fail — a partial
// import computes plausible-looking figures from incomplete data.
func reportWaitResult(r waitResult, label string) int {
	if len(r.failed) > 0 {
		for _, a := range r.failed {
			reason := "failed"
			switch {
			case a.Error != nil:
				reason = *a.Error
			case a.Message != nil:
				reason = *a.Message
			}
			output.Note(output.Red("✗") + fmt.Sprintf(" %s: %s", a.label(), reason))
		}
		return output.Failure
	}
	for _, a := range r.degraded {
		reason := "completed with warnings"
		if a.Message != nil {
			reason = *a.Message
		}
		output.Note(output.Yellow("!") + fmt.Sprintf(" %s: %s", a.label(), reason))
	}
	output.Note(output.Green("✓") + " " + label)
	return output.Ok
}
