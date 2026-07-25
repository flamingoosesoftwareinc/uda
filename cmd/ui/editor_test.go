package ui

import (
	"testing"

	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/stretchr/testify/require"
)

var testPos = analyzer.Position{
	File:     "main.go",
	Line:     10,
	ColStart: 2,
	ColEnd:   15,
}

func TestStrategyFor_Blocking(t *testing.T) {
	t.Parallel()

	for _, editor := range []string{"vim", "nvim", "vi", "nano", "emacs", "helix", "/usr/bin/nvim"} {
		s := strategyFor(editor)
		require.True(t, s.blocking(), "expected %q to be blocking", editor)
	}
}

func TestStrategyFor_FireAndForget(t *testing.T) {
	t.Parallel()

	for _, editor := range []string{"code", "subl", "zed", "/usr/bin/code"} {
		s := strategyFor(editor)
		require.False(t, s.blocking(), "expected %q to be fire-and-forget", editor)
	}
}

func TestEditorCmd_Vim(t *testing.T) {
	t.Parallel()

	s := strategyFor("nvim")
	c := s.cmd(testPos)
	require.Equal(t, []string{"+10", "main.go"}, c.Args[1:])
}

func TestEditorCmd_Nano(t *testing.T) {
	t.Parallel()

	s := strategyFor("nano")
	c := s.cmd(testPos)
	require.Equal(t, []string{"+10,2", "main.go"}, c.Args[1:])
}

func TestEditorCmd_Code(t *testing.T) {
	t.Parallel()

	s := strategyFor("code")
	c := s.cmd(testPos)
	require.Equal(t, []string{"--goto", "main.go:10:2"}, c.Args[1:])
}

func TestEditorCmd_Subl(t *testing.T) {
	t.Parallel()

	s := strategyFor("subl")
	c := s.cmd(testPos)
	require.Equal(t, []string{"main.go:10:2"}, c.Args[1:])
}

func TestDetectEditor_Env(t *testing.T) {
	t.Setenv("VISUAL", "code")
	t.Setenv("EDITOR", "vim")
	// VISUAL takes precedence
	s := detectEditor()
	require.False(t, s.blocking())
}

func TestDetectEditor_Fallback(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")

	s := detectEditor()
	require.True(t, s.blocking()) // falls back to vi
}
