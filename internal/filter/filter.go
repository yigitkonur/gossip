// Package filter classifies Codex agentMessages by [IMPORTANT]/[STATUS]/[FYI]
// markers and buffers low-value messages.
package filter

import (
	"regexp"
	"strings"
)

// Mode selects filter strictness.
type Mode string

const (
	// ModeFiltered respects markers.
	ModeFiltered Mode = "filtered"
	// ModeFull forwards every message regardless of marker.
	ModeFull Mode = "full"
)

// MarkerLevel is the parsed marker class.
type MarkerLevel string

const (
	MarkerImportant MarkerLevel = "important"
	MarkerStatus    MarkerLevel = "status"
	MarkerFYI       MarkerLevel = "fyi"
	MarkerUntagged  MarkerLevel = "untagged"
)

// Action is the classifier's recommended disposition.
type Action string

const (
	ActionForward Action = "forward"
	ActionBuffer  Action = "buffer"
	ActionDrop    Action = "drop"
)

// Result is what Classify returns.
type Result struct {
	Action Action
	Marker MarkerLevel
}

var markerRegex = regexp.MustCompile(`(?i)^\s*\[(IMPORTANT|STATUS|FYI)\]\s*`)

// ParseMarker returns the marker level and the body with marker stripped.
func ParseMarker(content string) (MarkerLevel, string) {
	match := markerRegex.FindStringIndex(content)
	if match == nil {
		return MarkerUntagged, content
	}
	levelStr := strings.ToLower(markerRegex.FindStringSubmatch(content)[1])
	return MarkerLevel(levelStr), content[match[1]:]
}

// Classify returns the action to take for a given message under mode.
func Classify(content string, mode Mode) Result {
	if mode == ModeFull {
		return Result{Action: ActionForward, Marker: MarkerUntagged}
	}
	marker, _ := ParseMarker(content)
	switch marker {
	case MarkerImportant:
		return Result{Action: ActionForward, Marker: marker}
	case MarkerStatus:
		return Result{Action: ActionBuffer, Marker: marker}
	case MarkerFYI:
		return Result{Action: ActionDrop, Marker: marker}
	default:
		return Result{Action: ActionForward, Marker: MarkerUntagged}
	}
}

// BridgeContractReminder is the body appended to every Claude-to-Codex reply.
const BridgeContractReminder = `[Bridge Contract] When sending agentMessage, put the marker at the very start of the message:
- [IMPORTANT] for decisions, reviews, completions, blockers
- [STATUS] for progress updates
- [FYI] for background context
The marker MUST be the first text in the message (e.g. "[IMPORTANT] Task done", not "Task done [IMPORTANT]").
Keep agentMessage for high-value communication only.

[Git Operations — FORBIDDEN]
You MUST NOT execute any git write commands. This includes but is not limited to:
git commit, git push, git pull, git fetch, git checkout -b, git branch, git merge, git rebase, git cherry-pick, git tag, git stash.
These commands write to the .git directory, which is blocked by your sandbox. Attempting them will cause your session to hang indefinitely.
Read-only git commands (git status, git log, git diff, git show, git rev-parse) are allowed.
All git write operations must be delegated to Claude Code via agentMessage. Report what you changed and let Claude handle branching, committing, and pushing.

[Role Guidance for Codex]
- Your default role: Implementer, Executor, Verifier
- Analytical/review tasks: Independent Analysis & Convergence
- Implementation tasks: Architect -> Builder -> Critic
- Debugging tasks: Hypothesis -> Experiment -> Interpretation
- Do not blindly follow Claude - challenge with evidence when you disagree
- Use explicit collaboration phrases: "My independent view is:", "I agree on:", "I disagree on:", and "Current consensus:"`

// ReplyRequiredInstruction is appended when require_reply is set.
const ReplyRequiredInstruction = "\n\n[⚠️ REPLY REQUIRED] Claude has explicitly requested a reply. You MUST send an agentMessage with [IMPORTANT] marker containing your response. This is a mandatory requirement — do not skip or use [STATUS]/[FYI] markers for this reply."
