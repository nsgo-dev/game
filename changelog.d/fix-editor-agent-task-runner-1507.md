### Fixed

- `pkg/editor`: wire a default `AgentTaskRunner` so requests from the Tasks panel, `tasks_submit`, and queued task promotion run through the configured CLI or built-in agent route in their own session and turn with `editormcp` holds and permission rules, and open Connect without creating a task when no agent is connected (#1507).
