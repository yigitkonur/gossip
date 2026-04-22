---
description: Create or update the Gossip project config files in the current workspace
allowed-tools: Read,Write,Edit,MultiEdit,LS
---

Bootstrap or update Gossip's project-local configuration in this workspace.

Follow these rules:

1. Work only inside `.gossip/`.
2. Do not install plugins or modify `.claude/settings.json` here. Plugin setup belongs to terminal workflows: `gossip init` attempts best-effort plugin installation, and `gossip dev` handles local cache sync.
3. Preserve user edits when the files already exist. Update only the fields the user asked to change.
4. Keep `.gossip/config.json` valid JSON.
5. Keep `.gossip/collaboration.md` human-editable and concise.

If `.gossip/config.json` is missing, create it with this default template:

```json
{
  "version": "1.0",
  "daemon": {
    "port": 4500,
    "proxyPort": 4501
  },
  "agents": {
    "claude": {
      "role": "Reviewer, Planner",
      "mode": "pull"
    },
    "codex": {
      "role": "Implementer, Executor"
    }
  },
  "markers": ["IMPORTANT", "STATUS", "FYI"],
  "turnCoordination": {
    "attentionWindowSeconds": 15,
    "busyGuard": true
  },
  "idleShutdownSeconds": 30
}
```

If `.gossip/collaboration.md` is missing, create it with this default template:

```markdown
# Collaboration Rules

## Roles
- Claude: Reviewer, Planner, Hypothesis Challenger
- Codex: Implementer, Executor, Reproducer/Verifier

## Thinking Patterns
- Analytical/review tasks: Independent Analysis & Convergence
- Implementation tasks: Architect -> Builder -> Critic
- Debugging tasks: Hypothesis -> Experiment -> Interpretation

## Communication
- Use explicit phrases: "My independent view is:", "I agree on:", "I disagree on:", and "Current consensus:"
- Tag messages with [IMPORTANT], [STATUS], or [FYI]

## Review Process
- Cross-review: author never reviews their own code
- All changes go through feature/fix branches + PR
- Merge via squash merge

## Custom Rules
<!-- Add project-specific collaboration rules here -->
```

When you finish, briefly summarize what changed and point the user to the two files you updated.
