package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
)

// editorOpenedMsg is sent after a fire-and-forget editor is spawned.
type editorOpenedMsg struct{}

// editorFinishedMsg is sent when a blocking editor exits.
type editorFinishedMsg struct{ err error }

// editorStrategy describes how to launch an editor.
type editorStrategy interface {
	// cmd builds the exec.Cmd for opening the given position.
	cmd(pos analyzer.Position) *exec.Cmd
	// blocking returns true if the editor takes over the terminal.
	blocking() bool
}

// detectEditor returns the strategy for the user's preferred editor.
// It checks $VISUAL, then $EDITOR, then falls back to "vi".
func detectEditor() editorStrategy {
	for _, envVar := range []string{"VISUAL", "EDITOR"} {
		if val := os.Getenv(envVar); val != "" {
			return strategyFor(val)
		}
	}

	return strategyFor("vi")
}

// strategyFor returns the appropriate strategy for a given editor command.
func strategyFor(command string) editorStrategy {
	base := filepath.Base(command)
	switch base {
	case "code", "subl", "zed":
		return fireAndForgetEditor{command: command}
	default:
		return blockingEditor{command: command}
	}
}

// blockingEditor suspends the TUI while the editor runs.
type blockingEditor struct {
	command string
}

func (e blockingEditor) blocking() bool { return true }

func (e blockingEditor) cmd(pos analyzer.Position) *exec.Cmd {
	base := filepath.Base(e.command)
	switch base {
	case "nano":
		//nolint:noctx,gosec // interactive editor invocation; argv is operator-controlled editor + validated position fields.
		return exec.Command(e.command,
			fmt.Sprintf("+%d,%d", pos.Line, pos.ColStart),
			pos.File,
		)
	default:
		//nolint:noctx,gosec // interactive editor invocation; argv is operator-controlled editor + validated position fields.
		// vim, nvim, emacs, helix, vi
		return exec.Command(e.command,
			fmt.Sprintf("+%d", pos.Line),
			pos.File,
		)
	}
}

// fireAndForgetEditor spawns the editor without blocking.
type fireAndForgetEditor struct {
	command string
}

func (e fireAndForgetEditor) blocking() bool { return false }

func (e fireAndForgetEditor) cmd(pos analyzer.Position) *exec.Cmd {
	base := filepath.Base(e.command)
	switch base {
	case "code":
		//nolint:noctx,gosec // detached editor launch; argv is operator-controlled editor + validated position fields.
		return exec.Command(e.command, "--goto",
			fmt.Sprintf("%s:%d:%d", pos.File, pos.Line, pos.ColStart),
		)
	default:
		//nolint:noctx,gosec // detached editor launch; argv is operator-controlled editor + validated position fields.
		// subl, zed
		return exec.Command(e.command,
			fmt.Sprintf("%s:%d:%d", pos.File, pos.Line, pos.ColStart),
		)
	}
}

// openEditor returns a tea.Cmd that opens the given position in the user's
// preferred editor. rootDir is joined with pos.File to form an absolute path.
func openEditor(pos analyzer.Position, rootDir string) tea.Cmd {
	pos.File = filepath.Join(rootDir, pos.File)
	editor := detectEditor()
	c := editor.cmd(pos)

	if editor.blocking() {
		return tea.ExecProcess(c, func(err error) tea.Msg {
			return editorFinishedMsg{err: err}
		})
	}

	return func() tea.Msg {
		_ = c.Start()

		return editorOpenedMsg{}
	}
}
