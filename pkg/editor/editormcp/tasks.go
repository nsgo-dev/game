package editormcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/nsgo-dev/game/pkg/editor"
)

const (
	// ToolTasksList lists all background agent tasks, active object holds, and summary badge counts.
	ToolTasksList = "tasks_list"
	// ToolTasksSubmit queues or runs a background agent request in parallel.
	ToolTasksSubmit = "tasks_submit"
	// ToolTasksControl controls background tasks (stop and revert of the session's own task, dismiss of failed tasks, retry, open, focus). Its keep, allow and deny actions are always refused: keeping a review and answering a permission prompt are the person's decisions.
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
	`"action":{"type":"string","enum":["keep","revert","dismiss","stop","retry","open","focus","allow","deny"],"description":"Action to perform on task_id or task_ids; keep, allow and deny are the person's decisions and are always refused, stop and revert apply only to this session's own task and stop never to one that is Ready to review, and dismiss applies only to failed tasks"},` +
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
			Description: "Retry, focus, or open background agent tasks, stop or revert this session's own task, or dismiss failed tasks. Keeping a task, stopping or dismissing one that is Ready to review, and answering a permission prompt (keep, allow, deny) are the person's decisions and are refused.",
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
		case "keep", "allow", "deny":
			// Keeping a review and answering a permission prompt are the person's: refuse before reaching the editor, which refuses agents as well.
			return fmt.Errorf("%s of task %s is the person's decision: %w", args.Action, args.TaskID, editor.ErrTaskDecisionYouOnly)
		case "revert":
			// Reverting another task's pending work is the person's decision; a session may revert only the task it is bound to.
			if c.taskID == "" || args.TaskID != c.taskID {
				return fmt.Errorf("revert of task %s is the person's decision: this session may revert only its own task", args.TaskID)
			}
			if err := ed.RevertTask(args.TaskID); err != nil {
				return err
			}
			msg = fmt.Sprintf("Reverted task %s", args.TaskID)
		case "dismiss":
			ids := args.TaskIDs
			if len(ids) == 0 && args.TaskID != "" {
				ids = []string{args.TaskID}
			}
			for _, id := range ids {
				if task, ok := ed.Task(id); ok && task.State == editor.TaskReadyToReview && !task.Dismissed {
					return fmt.Errorf("dismissing task %s, which is Ready to review, is the person's decision: %w", id, editor.ErrTaskDecisionYouOnly)
				}
			}
			n := ed.DismissTasks(ids)
			msg = fmt.Sprintf("Dismissed %d tasks (%s)", n, strings.Join(ids, ", "))
		case "stop":
			// Stopping ends a task without reverting its edits, so a session may stop only the task it is bound to, and never once it is Ready to review (StopTask refuses that too).
			if c.taskID == "" || args.TaskID != c.taskID {
				return fmt.Errorf("stop of task %s is the person's decision: this session may stop only its own task", args.TaskID)
			}
			if task, ok := ed.Task(args.TaskID); ok && task.State == editor.TaskReadyToReview {
				return fmt.Errorf("stopping task %s, which is Ready to review, is the person's decision: %w", args.TaskID, editor.ErrTaskDecisionYouOnly)
			}
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
