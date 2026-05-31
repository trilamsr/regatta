package spawner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// processKiller is the seam tests use to inject classified Kill errors
// without spawning real processes. Production code uses the default
// (*exec.Cmd).Process.Kill via defaultKiller.
type processKiller func(*exec.Cmd) error

func defaultKiller(c *exec.Cmd) error { return c.Process.Kill() }

// ClaudeSpawner launches an agent process inside a per-agent
// worktree. It implements the Spawner interface.
//
// Restart-adoption gap (issue #45): on orchestrator restart the
// children map is empty. Agents whose claude process is still alive
// pass pidAlive and stay in `running`, but KillAgent returns
// (false, nil) because the *exec.Cmd handle was lost when the
// parent died. The Reaper still removes the worktree on a terminal
// transition; the live child is reaped lazily by the kernel
// parent-death cascade or an operator pkill until the adoption
// routine ships under #45.
//
// The spawn sequence is:
//
//  1. WorktreeManager.Create(agentID, BaseRef) - deterministic path
//  2. PromptBuilder(req) - assemble the templated prompt
//  3. exec the configured Command with the prompt on stdin
//  4. parse a session-id from stdout (or fall back to a synthetic
//     "claude-<agent>" string when the binary does not emit one)
//  5. return Result{PID, SessionID}; the child is left running
//
// On any step failure the worktree is reaped so the next tick can
// retry from a clean state. The orchestrator's existing rollback
// (spawning → crashed → pending) handles state recovery.
type ClaudeSpawner struct {
	wm      *WorktreeManager
	cfg     ClaudeSpawnerConfig
	starter ProcessStarter
	killer  processKiller

	mu       sync.Mutex
	children map[int64]*exec.Cmd
}

// ClaudeSpawnerConfig holds the tunables. Zero values default to
// sensible production settings.
type ClaudeSpawnerConfig struct {
	// Command is the claude binary path. Default: "claude".
	Command string

	// Args are extra arguments appended after Command. Default: none.
	// The agent's prompt is always passed on stdin so callers do not
	// need to template-encode it into argv.
	Args []string

	// BaseRef is the git ref the worktree branches off. Default:
	// "HEAD". Production setups pin to "main" or "origin/main".
	BaseRef string

	// Prompt assembles the prompt sent to the child. nil falls back
	// to a minimal default that names the work item.
	Prompt PromptBuilder
}

// PromptBuilder produces the prompt text for one Spawn request.
type PromptBuilder func(Request) string

// ProcessStarter is the seam tests use to stub `exec.Command`. The
// returned *exec.Cmd MUST have Process populated (via Start). The
// caller does NOT Wait on the process; that is the agent runtime's
// concern.
type ProcessStarter func(ctx context.Context, name string, args []string, stdin io.Reader, dir string) (*exec.Cmd, error)

// NewClaudeSpawner constructs a Spawner.
func NewClaudeSpawner(wm *WorktreeManager, cfg ClaudeSpawnerConfig) (*ClaudeSpawner, error) {
	if wm == nil {
		return nil, errors.New("spawner: ClaudeSpawner requires a WorktreeManager")
	}
	if cfg.Command == "" {
		cfg.Command = "claude"
	}
	if cfg.BaseRef == "" {
		cfg.BaseRef = "HEAD"
	}
	if cfg.Prompt == nil {
		cfg.Prompt = defaultPromptBuilder
	}
	return &ClaudeSpawner{
		wm:       wm,
		cfg:      cfg,
		starter:  execStarter,
		killer:   defaultKiller,
		children: map[int64]*exec.Cmd{},
	}, nil
}

// SetStarter overrides the process-start seam. Tests inject a fake
// that starts a script binary; production code MUST NOT call this.
func (s *ClaudeSpawner) SetStarter(p ProcessStarter) {
	if p != nil {
		s.starter = p
	}
}

// Spawn runs the create-prompt-exec sequence. The returned Result
// records the child's PID + the session-id parsed from its stdout
// (or a synthetic fallback). The child is detached from the
// orchestrator's lifetime; the Reaper owns teardown.
func (s *ClaudeSpawner) Spawn(ctx context.Context, req Request) (Result, error) {
	path, err := s.wm.Create(ctx, req.AgentID, s.cfg.BaseRef)
	if err != nil {
		return Result{}, fmt.Errorf("spawner: create worktree: %w", err)
	}
	prompt := s.cfg.Prompt(req)

	args := append([]string(nil), s.cfg.Args...)
	cmd, err := s.starter(ctx, s.cfg.Command, args, strings.NewReader(prompt), path)
	if err != nil {
		_ = s.wm.Remove(context.WithoutCancel(ctx), req.AgentID)
		return Result{}, fmt.Errorf("spawner: start claude: %w", err)
	}
	if cmd.Process == nil {
		_ = s.wm.Remove(context.WithoutCancel(ctx), req.AgentID)
		return Result{}, errors.New("spawner: starter returned cmd with nil Process")
	}
	pid := cmd.Process.Pid

	// Session-id capture is a follow-up: the claude CLI emits its
	// own session-id but the exact format varies by version, and
	// reading cmd.Stdout immediately after Start races against the
	// child's first write. For now we use a deterministic synthetic
	// id so `claude --resume <id>` can be wired once the format is
	// pinned. Tracked in issue #27 follow-up.
	sessionID := fmt.Sprintf("claude-%d", req.AgentID)

	s.mu.Lock()
	s.children[req.AgentID] = cmd
	s.mu.Unlock()

	return Result{PID: pid, SessionID: sessionID}, nil
}

// Children returns the live exec.Cmd handles by agent ID. The Reaper
// uses this to send SIGTERM during teardown.
func (s *ClaudeSpawner) Children() map[int64]*exec.Cmd {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[int64]*exec.Cmd, len(s.children))
	for k, v := range s.children {
		out[k] = v
	}
	return out
}

// Forget removes the agent from the child map after the Reaper has
// torn it down. Safe to call on an unknown ID.
func (s *ClaudeSpawner) Forget(agentID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.children, agentID)
}

// KillAgent implements the reaper.ChildKiller contract. Returns
// signaled=true when the agent was known and a kill signal was sent
// to its process. The agent is forgotten from the child map either
// way so a subsequent Spawn for the same ID does not double-track.
//
// Skeleton uses cmd.Process.Kill (SIGKILL on Unix, TerminateProcess
// on Windows) for portability. The design.md §SupervisorLimits
// contract calls for SIGTERM-then-SIGKILL escalation; that lands
// alongside the SupervisorLimits work tracked in issue #28.
func (s *ClaudeSpawner) KillAgent(agentID int64) (bool, error) {
	s.mu.Lock()
	cmd, ok := s.children[agentID]
	delete(s.children, agentID)
	s.mu.Unlock()
	if !ok || cmd == nil || cmd.Process == nil {
		return false, nil
	}
	if err := s.killer(cmd); err != nil {
		if errors.Is(err, os.ErrProcessDone) {
			return true, nil
		}
		return false, err
	}
	return true, nil
}

// WorktreeManager exposes the underlying manager so the Reaper can
// remove worktrees without re-discovering the path.
func (s *ClaudeSpawner) WorktreeManager() *WorktreeManager { return s.wm }

func defaultPromptBuilder(req Request) string {
	return fmt.Sprintf("regatta: work item %s on lane %s (agent %d). Follow the repo's acceptance criteria and open a PR when CI is green.",
		req.WorkItemID, req.Lane, req.AgentID)
}

// execStarter is the production ProcessStarter. It runs the binary
// in dir, with stdin piped from prompt, and forwards stdout/stderr
// to the orchestrator's own streams so operators can tail them.
func execStarter(ctx context.Context, name string, args []string, stdin io.Reader, dir string) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdin = stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}
