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

// processKiller is the seam tests use to inject classified Kill errors.
type processKiller func(*exec.Cmd) error

func defaultKiller(c *exec.Cmd) error { return c.Process.Kill() }

// ClaudeSpawner launches an agent process inside a per-agent worktree.
// On restart the children map is empty, so KillAgent returns (false, nil)
// for surviving children until the adoption work in issue #45 ships.
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

	// Args are extra arguments appended after Command. The prompt is
	// always passed on stdin.
	Args []string

	// BaseRef is the git ref the worktree branches off. Default: "HEAD".
	BaseRef string

	// Prompt assembles the prompt sent to the child. Default: a minimal
	// builder that names the work item.
	Prompt PromptBuilder
}

// PromptBuilder produces the prompt text for one Spawn request.
type PromptBuilder func(Request) string

// ProcessStarter stubs exec.Command for tests. The returned *exec.Cmd
// MUST have Process populated (i.e. Start has already been called).
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

// SetStarter overrides the process-start seam. Tests only.
func (s *ClaudeSpawner) SetStarter(p ProcessStarter) {
	if p != nil {
		s.starter = p
	}
}

// Spawn creates the worktree, execs the configured command with the
// templated prompt on stdin, and returns the child's PID + session-id.
// Worktree teardown is owned by the Reaper.
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

	// Synthetic session id until the claude CLI's emitted format is
	// pinned across versions; tracked in issue #27.
	sessionID := fmt.Sprintf("claude-%d", req.AgentID)

	s.mu.Lock()
	s.children[req.AgentID] = cmd
	s.mu.Unlock()

	return Result{PID: pid, SessionID: sessionID}, nil
}

// Children returns a snapshot of live exec.Cmd handles by agent ID.
func (s *ClaudeSpawner) Children() map[int64]*exec.Cmd {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[int64]*exec.Cmd, len(s.children))
	for k, v := range s.children {
		out[k] = v
	}
	return out
}

// Forget drops the agent from the child map. Safe on unknown IDs.
func (s *ClaudeSpawner) Forget(agentID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.children, agentID)
}

// KillAgent implements reaper.ChildKiller. The agent is forgotten from
// the child map either way so a re-Spawn does not double-track.
// SIGTERM-then-SIGKILL escalation lands with SupervisorLimits (#28).
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

// WorktreeManager exposes the underlying manager for the Reaper.
func (s *ClaudeSpawner) WorktreeManager() *WorktreeManager { return s.wm }

func defaultPromptBuilder(req Request) string {
	return fmt.Sprintf("regatta: work item %s on lane %s (agent %d). Follow the repo's acceptance criteria and open a PR when CI is green.",
		req.WorkItemID, req.Lane, req.AgentID)
}

// execStarter is the production ProcessStarter. Stdout/stderr forward
// to the orchestrator's streams so operators can tail them.
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
