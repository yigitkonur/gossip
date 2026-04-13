package config

// DefaultConfig is the factory default for new .agentbridge/config.json files.
var DefaultConfig = Config{
	Version: "1.0",
	Daemon:  DaemonConfig{Port: 4500, ProxyPort: 4501},
	Agents: map[string]AgentConfig{
		"claude": {Role: "Reviewer, Planner", Mode: "push"},
		"codex":  {Role: "Implementer, Executor"},
	},
	Markers: []string{"IMPORTANT", "STATUS", "FYI"},
	TurnCoordination: TurnCoordinationConfig{
		AttentionWindowSeconds: 15,
		BusyGuard:              true,
	},
	IdleShutdownSeconds: 30,
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
