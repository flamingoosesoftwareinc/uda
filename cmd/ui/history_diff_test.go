package ui

import (
	"testing"

	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/flamingoosesoftwareinc/uda/internal/diff"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMapDiffType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   diff.Type
		want diffType
	}{
		{"unchanged", diff.Unchanged, diffUnchanged},
		{"added", diff.Added, diffAdded},
		{"removed", diff.Removed, diffRemoved},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, mapDiffType(tt.in))
		})
	}
}

func TestSymbolDiffToItems(t *testing.T) {
	t.Parallel()

	prevStats := analyzer.CouplingStats{
		"fmt.Errorf": {
			Count: 2,
			Positions: []analyzer.Position{
				{File: "main.go", Line: 10},
				{File: "main.go", Line: 20},
			},
		},
	}

	currStats := analyzer.CouplingStats{
		"fmt.Errorf": {
			Count: 3,
			Positions: []analyzer.Position{
				{File: "main.go", Line: 10},
				{File: "main.go", Line: 30},
				{File: "main.go", Line: 40},
			},
		},
	}

	symbolDiffs := diff.Symbols(prevStats, currStats)
	items := symbolDiffToItems(symbolDiffs, "prev123", "curr456", prevStats, currStats)

	require.Len(t, items, 1)
	item := items[0]

	// Should show count change
	assert.Contains(t, item.text, "x3 (+1)")

	// Should have both stats for expansion
	assert.NotNil(t, item.stat)
	assert.NotNil(t, item.prevStat)
	assert.Equal(t, uint(3), item.stat.Count)
	assert.Equal(t, uint(2), item.prevStat.Count)

	// Should have both SHAs
	assert.Equal(t, "curr456", item.sha)
	assert.Equal(t, "prev123", item.prevSHA)
}

func TestSymbolDiffToItems_AddedSymbol(t *testing.T) {
	t.Parallel()

	prevStats := analyzer.CouplingStats{}
	currStats := analyzer.CouplingStats{
		"fmt.Errorf": {
			Count: 2,
			Positions: []analyzer.Position{
				{File: "main.go", Line: 10},
			},
		},
	}

	symbolDiffs := diff.Symbols(prevStats, currStats)
	items := symbolDiffToItems(symbolDiffs, "prev123", "curr456", prevStats, currStats)

	require.Len(t, items, 1)
	item := items[0]

	assert.Equal(t, diffAdded, item.diffType)
	assert.Nil(t, item.prevStat, "added symbol should not have prevStat")
	assert.Equal(t, "curr456", item.sha)
}

func TestSymbolDiffToItems_RemovedSymbol(t *testing.T) {
	t.Parallel()

	prevStats := analyzer.CouplingStats{
		"fmt.Errorf": {
			Count: 2,
			Positions: []analyzer.Position{
				{File: "main.go", Line: 10},
			},
		},
	}
	currStats := analyzer.CouplingStats{}

	symbolDiffs := diff.Symbols(prevStats, currStats)
	items := symbolDiffToItems(symbolDiffs, "prev123", "curr456", prevStats, currStats)

	require.Len(t, items, 1)
	item := items[0]

	assert.Equal(t, diffRemoved, item.diffType)
	assert.Nil(t, item.prevStat, "removed symbol should not have prevStat")
	assert.Equal(t, "prev123", item.sha, "removed symbol should use prevSHA")
}

func TestPositionDiffToItems(t *testing.T) {
	t.Parallel()

	posDiffs := diff.Positions(
		[]analyzer.Position{
			{File: "main.go", Line: 10, ColStart: 5, ColEnd: 15},
			{File: "main.go", Line: 20, ColStart: 1, ColEnd: 10},
		},
		[]analyzer.Position{
			{File: "main.go", Line: 10, ColStart: 5, ColEnd: 15},
			{File: "main.go", Line: 30, ColStart: 1, ColEnd: 10},
		},
	)

	items := positionDiffToItems(posDiffs, "abc123", "def456")

	require.Len(t, items, 3)

	var added, removed, unchanged int

	for _, item := range items {
		assert.NotNil(t, item.pos)

		switch item.diffType {
		case diffAdded:
			added++

			assert.Equal(t, "def456", item.sha)
		case diffRemoved:
			removed++

			assert.Equal(t, "abc123", item.sha)
		case diffUnchanged:
			unchanged++

			assert.Equal(t, "def456", item.sha)
		}
	}

	assert.Equal(t, 1, added)
	assert.Equal(t, 1, removed)
	assert.Equal(t, 1, unchanged)
}
