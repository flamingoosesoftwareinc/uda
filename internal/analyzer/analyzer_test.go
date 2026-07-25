package analyzer_test

import (
	"testing"

	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/stretchr/testify/require"
)

func TestStringInterner(t *testing.T) {
	t.Run("returns same pointer for identical strings", func(t *testing.T) {
		interner := analyzer.NewStringInterner()

		s1 := interner.Intern("src/main.rs")
		s2 := interner.Intern("src/main.rs")

		// Values should be equal
		require.Equal(t, s1, s2)
	})

	t.Run("returns different strings for different inputs", func(t *testing.T) {
		interner := analyzer.NewStringInterner()

		s1 := interner.Intern("src/main.rs")
		s2 := interner.Intern("src/lib.rs")

		require.NotEqual(t, s1, s2)
	})

	t.Run("handles empty string", func(t *testing.T) {
		interner := analyzer.NewStringInterner()

		s1 := interner.Intern("")
		s2 := interner.Intern("")

		require.Equal(t, s1, s2)
		require.Empty(t, s1)
	})
}
