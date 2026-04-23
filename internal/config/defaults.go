package config

// DefaultConfig is the factory default for new .gossip/config.json files.
var DefaultConfig = Config{
	Version: "1.0",
	Daemon:  DaemonConfig{Port: 4500, ProxyPort: 4501},
	Agents: map[string]AgentConfig{
		"claude": {Role: "Reviewer, Planner", Mode: "pull"},
		"codex":  {Role: "Implementer, Executor"},
	},
	Markers: []string{"IMPORTANT", "STATUS", "FYI"},
	TurnCoordination: TurnCoordinationConfig{
		AttentionWindowSeconds: 15,
		BusyGuard:              true,
	},
	IdleShutdownSeconds: 30,
	Loop: LoopConfig{
		Enabled:          true,
		MaxIterations:    5,
		PerTurnTimeoutMs: 90_000,
		// Restricted to a single explicit sentinel by default. `DONE` and
		// `READY` (earlier candidates) false-positive on ordinary prose —
		// "I'm done with the changes" would engage the loop. Projects that
		// want more aliases can add them in .gossip/config.json.
		CompletionTags: []string{"COMPLETION"},
		// `APPROVED` similarly matched "not approved" negations with the
		// word-boundary check; dropped from defaults. `COMPLETED`/`LGTM`
		// are idiomatic sign-offs that Codex would not emit accidentally.
		ApprovalTags: []string{"COMPLETED", "LGTM"},
	},
}

// DefaultCollaborationMD is the factory default for collaboration.md.
const DefaultCollaborationMD = `# Collaboration Rules

## Roles
- Claude: Reviewer, Planner, Hypothesis Challenger
- Codex: Implementer, Executor, Reproducer/Verifier

## Communication
- Use explicit phrases: "My independent view is:", "I agree on:", "I disagree on:", "Current consensus:"
- Tag messages with [IMPORTANT], [STATUS], or [FYI]

## Review Process
- Cross-review: author never reviews their own code
- All changes go through feature/fix branches + PR
- Merge via squash merge
`
