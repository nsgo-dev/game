package editormcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/nsgo-dev/game/packages/game/editor"
)

const (
	// ToolTasksList lists all background agent tasks, active object holds, and summary badge counts.
	ToolTasksList = "tasks_list"
	// ToolTasksSubmit queues or runs a background agent request in parallel.
	ToolTasksSubmit = "tasks_submit"
	// ToolTasksControl controls background tasks (keep, revert, dismiss, stop, retry, open, focus, allow, deny).
	ToolTasksControl = "tasks_control"
)

const tasksListSchema = `{"type":"object","additionalProperties":false,"properties":{` +
	`"include_dismissed":{"type":"boolean","description":"Include dismissed or completed tasks in addition to the five active groups"}` +
	`}}`

const tasksSubmitSchema = `{"type":"object","additionalProperties":false,"required":["prompt"],"properties":{` +
	`"title":{"type":"string","description":"Short task title; derived from prompt when omitted"},` +
	`"prompt":{"type":"string","description":"The agent request prompt"},` +
	`"mode":{"type":"string","enum":["queue","parallel"],"description":"queue (starts after the running turn is kept or reverted) or parallel (starts now with its own numbered overlay)"},` +
	`"scene":{"type":"string","description":"Optional target scene or file path"}` +
	`}}`

const tasksControlSchema = `{"type":"object","additionalProperties":false,"required":["action"],"properties":{` +
	`"action":{"type":"string","enum":["keep","revert","dismiss","stop","retry","open","focus","allow","deny"],"description":"Action to perform on task_id or task_ids"},` +
	`"task_id":{"type":"string","description":"Target task ID"},` +
	`"task_ids":{"type":"array","items":{"type":"string"},"description":"Target task IDs (for bulk dismiss)"}` +
	`}}`

// TasksListResult is the structured reply returned by tasks_list.
type TasksListResult struct {
	Summary editor.TaskSummary `json:"summary"`
	Tasks   []editor.AgentTask `json:"tasks"`
	Holds   map[string]string  `json:"holds,omitempty"`
}

func taskTools() []Tool {
	return []Tool{
		{
			Name:        ToolTasksList,
			Title:       "List background tasks",
			Description: "List background agent tasks grouped across Needs you, Running, Ready to review, Queued and Failed, along with active object holds and summary badge counts.",
			InputSchema: []byte(tasksListSchema),
			Action:      ActionRead,
			Handler:     tasksListTool,
		},
		{
			Name:        ToolTasksSubmit,
			Title:       "Submit a background task",
			Description: "Queue a background agent request (starts after the running turn is kept or reverted) or run it in parallel as a separate session with its own numbered overlay.",
			InputSchema: []byte(tasksSubmitSchema),
			Action:      ActionEdit,
			Handler:     tasksSubmitTool,
		},
		{
			Name:        ToolTasksControl,
			Title:       "Control background tasks",
			Description: "Keep, revert, bulk-dismiss, stop, retry, focus, open, or resolve permission on background agent tasks.",
			InputSchema: []byte(tasksControlSchema),
			Action:      ActionEdit,
			Handler:     tasksControlTool,
		},
	}
}

func tasksListTool(ctx context.Context, c *Call) (*Result, error) {
	var args struct {
		IncludeDismissed bool `json:"include_dismissed"`
	}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("tasks_list: %w", err)
	}
	var out TasksListResult
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		out.Summary = ed.TaskSummary()
		if args.IncludeDismissed {
			out.Tasks = ed.Tasks()
		} else {
			out.Tasks = ed.VisibleTasks()
		}
		return nil
	}); err != nil {
		return nil, err
	}
	out.Holds = c.s.HeldObjects()
	text := fmt.Sprintf("%d tasks (%d running, %d needs you, %d to review, %d queued, %d failed; dock badge %d)",
		len(out.Tasks), out.Summary.Running, out.Summary.NeedsYou, out.Summary.ReadyToReview, out.Summary.Queued, out.Summary.Failed, out.Summary.DockBadge)
	return &Result{Text: text, Structured: out}, nil
}

func tasksSubmitTool(ctx context.Context, c *Call) (*Result, error) {
	var args struct {
		Title  string `json:"title"`
		Prompt string `json:"prompt"`
		Mode   string `json:"mode"`
		Scene  string `json:"scene"`
	}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("tasks_submit: %w", err)
	}
	mode := editor.TaskStartQueue
	if args.Mode != "" {
		mode = editor.TaskStartMode(args.Mode)
	}
	var task editor.AgentTask
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		var err error
		task, err = ed.SubmitTask(editor.TaskRequest{
			Title:     args.Title,
			Prompt:    args.Prompt,
			Source:    editor.TaskSourceUser,
			StartMode: mode,
			ScenePath: args.Scene,
		})
		return err
	}); err != nil {
		return nil, fmt.Errorf("tasks_submit: %w", err)
	}
	return &Result{
		Text:       fmt.Sprintf("Submitted task %s (%s): %s", task.ID, task.State, task.Title),
		Structured: task,
	}, nil
}

func tasksControlTool(ctx context.Context, c *Call) (*Result, error) {
	var args struct {
		Action  string   `json:"action"`
		TaskID  string   `json:"task_id"`
		TaskIDs []string `json:"task_ids"`
	}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("tasks_control: %w", err)
	}
	if args.Action == "" {
		return nil, errors.New("tasks_control: action is required")
	}
	var summary editor.TaskSummary
	var msg string
	err := c.Editor(ctx, func(ed *editor.Editor) error {
		switch args.Action {
		case "keep":
			if err := ed.KeepTask(args.TaskID); err != nil {
				return err
			}
			msg = fmt.Sprintf("Kept task %s", args.TaskID)
		case "revert":
			if err := ed.RevertTask(args.TaskID); err != nil {
				return err
			}
			msg = fmt.Sprintf("Reverted task %s", args.TaskID)
		case "dismiss":
			ids := args.TaskIDs
			if len(ids) == 0 && args.TaskID != "" {
				ids = []string{args.TaskID}
			}
			n := ed.DismissTasks(ids)
			msg = fmt.Sprintf("Dismissed %d tasks (%s)", n, strings.Join(ids, ", "))
		case "stop":
			if err := ed.StopTask(args.TaskID); err != nil {
				return err
			}
			msg = fmt.Sprintf("Stopped task %s", args.TaskID)
		case "retry":
			if err := ed.RetryTask(args.TaskID); err != nil {
				return err
			}
			msg = fmt.Sprintf("Retried task %s", args.TaskID)
		case "open":
			if err := ed.OpenTask(args.TaskID); err != nil {
				return err
			}
			msg = fmt.Sprintf("Opened task %s", args.TaskID)
		case "focus":
			ed.FocusTask(args.TaskID)
			msg = fmt.Sprintf("Focused task %s", args.TaskID)
		case "allow":
			if err := ed.ResolveTaskPermission(args.TaskID, true); err != nil {
				return err
			}
			msg = fmt.Sprintf("Allowed permission for task %s", args.TaskID)
		case "deny":
			if err := ed.ResolveTaskPermission(args.TaskID, false); err != nil {
				return err
			}
			msg = fmt.Sprintf("Denied permission for task %s", args.TaskID)
		default:
			return fmt.Errorf("unknown action %q", args.Action)
		}
		summary = ed.TaskSummary()
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("tasks_control: %w", err)
	}
	return &Result{Text: msg, Structured: summary}, nil
}
