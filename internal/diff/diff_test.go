package diff

import (
	"testing"

	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/sebdah/goldie/v2"
	"github.com/stretchr/testify/require"
)

func TestDiffPositions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		prev []analyzer.Position
		curr []analyzer.Position
	}{
		{
			name: "all_added",
			prev: nil,
			curr: []analyzer.Position{
				{File: "main.go", Line: 10, ColStart: 5, ColEnd: 15},
				{File: "main.go", Line: 20, ColStart: 1, ColEnd: 10},
			},
		},
		{
			name: "all_removed",
			prev: []analyzer.Position{
				{File: "main.go", Line: 10, ColStart: 5, ColEnd: 15},
			},
			curr: nil,
		},
		{
			name: "mixed",
			prev: []analyzer.Position{
				{File: "main.go", Line: 10, ColStart: 5, ColEnd: 15}, // unchanged
				{File: "main.go", Line: 20, ColStart: 1, ColEnd: 10}, // removed
				{File: "other.go", Line: 5, ColStart: 1, ColEnd: 10}, // removed
			},
			curr: []analyzer.Position{
				{File: "main.go", Line: 10, ColStart: 5, ColEnd: 15}, // unchanged
				{File: "main.go", Line: 30, ColStart: 1, ColEnd: 10}, // added
				{File: "new.go", Line: 1, ColStart: 1, ColEnd: 5},    // added
			},
		},
		{
			name: "column_shift_unchanged",
			prev: []analyzer.Position{
				{File: "main.go", Line: 10, ColStart: 5, ColEnd: 15},
			},
			curr: []analyzer.Position{
				{File: "main.go", Line: 10, ColStart: 10, ColEnd: 25},
			},
		},
		{
			name: "both_empty",
			prev: nil,
			curr: nil,
		},
	}

	g := goldie.New(t,
		goldie.WithFixtureDir(".testdata"),
		goldie.WithNameSuffix(".json"),
	)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := Positions(tt.prev, tt.curr)
			g.AssertJson(t, "positions/"+tt.name, result)
		})
	}
}

func TestDiffSymbols(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		prev analyzer.CouplingStats
		curr analyzer.CouplingStats
	}{
		{
			name: "added_symbol",
			prev: analyzer.CouplingStats{},
			curr: analyzer.CouplingStats{
				"fmt.Errorf": {
					Count: 2,
					Positions: []analyzer.Position{
						{File: "main.go", Line: 10},
					},
				},
			},
		},
		{
			name: "removed_symbol",
			prev: analyzer.CouplingStats{
				"fmt.Errorf": {
					Count: 2,
					Positions: []analyzer.Position{
						{File: "main.go", Line: 10},
					},
				},
			},
			curr: analyzer.CouplingStats{},
		},
		{
			name: "count_changed",
			prev: analyzer.CouplingStats{
				"fmt.Errorf": {
					Count: 2,
					Positions: []analyzer.Position{
						{File: "main.go", Line: 10},
						{File: "main.go", Line: 20},
					},
				},
			},
			curr: analyzer.CouplingStats{
				"fmt.Errorf": {
					Count: 3,
					Positions: []analyzer.Position{
						{File: "main.go", Line: 10},
						{File: "main.go", Line: 30},
						{File: "main.go", Line: 40},
					},
				},
			},
		},
		{
			name: "unchanged",
			prev: analyzer.CouplingStats{
				"fmt.Errorf": {
					Count:     1,
					Positions: []analyzer.Position{{File: "main.go", Line: 10}},
				},
			},
			curr: analyzer.CouplingStats{
				"fmt.Errorf": {
					Count:     1,
					Positions: []analyzer.Position{{File: "main.go", Line: 10}},
				},
			},
		},
		{
			name: "multiple_mixed",
			prev: analyzer.CouplingStats{
				"fmt.Errorf": {Count: 1, Positions: []analyzer.Position{{File: "a.go", Line: 1}}},
				"fmt.Println": {
					Count: 2,
					Positions: []analyzer.Position{
						{File: "a.go", Line: 2},
						{File: "a.go", Line: 3},
					},
				},
			},
			curr: analyzer.CouplingStats{
				"fmt.Errorf":  {Count: 1, Positions: []analyzer.Position{{File: "a.go", Line: 1}}},
				"fmt.Sprintf": {Count: 1, Positions: []analyzer.Position{{File: "b.go", Line: 5}}},
			},
		},
	}

	g := goldie.New(t,
		goldie.WithFixtureDir(".testdata"),
		goldie.WithNameSuffix(".json"),
	)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := Symbols(tt.prev, tt.curr)
			g.AssertJson(t, "symbols/"+tt.name, result)
		})
	}
}

func TestDiffCouplingStats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		prev analyzer.PackageCouplingStats
		curr analyzer.PackageCouplingStats
	}{
		{
			name: "new_edge",
			prev: nil,
			curr: analyzer.PackageCouplingStats{
				"pkg/http": analyzer.CouplingStats{
					"http.Client": {
						Count:     1,
						Positions: []analyzer.Position{{File: "server.go", Line: 42}},
					},
				},
			},
		},
		{
			name: "removed_edge",
			prev: analyzer.PackageCouplingStats{
				"pkg/legacy": analyzer.CouplingStats{
					"legacy.Transform": {
						Count:     1,
						Positions: []analyzer.Position{{File: "handler.go", Line: 12}},
					},
				},
			},
			curr: nil,
		},
		{
			name: "changed_edge",
			prev: analyzer.PackageCouplingStats{
				"pkg/config": analyzer.CouplingStats{
					"config.Debug": {
						Count:     1,
						Positions: []analyzer.Position{{File: "app.go", Line: 18}},
					},
				},
			},
			curr: analyzer.PackageCouplingStats{
				"pkg/config": analyzer.CouplingStats{
					"config.Timeout": {
						Count:     1,
						Positions: []analyzer.Position{{File: "server.go", Line: 30}},
					},
					"config.MaxRetries": {
						Count:     1,
						Positions: []analyzer.Position{{File: "server.go", Line: 31}},
					},
				},
			},
		},
		{
			name: "mixed_edges",
			prev: analyzer.PackageCouplingStats{
				"pkg/db":     analyzer.CouplingStats{"db.Query": {Count: 2}},
				"pkg/legacy": analyzer.CouplingStats{"legacy.Old": {Count: 1}},
			},
			curr: analyzer.PackageCouplingStats{
				"pkg/db":    analyzer.CouplingStats{"db.Query": {Count: 3}},
				"pkg/cache": analyzer.CouplingStats{"cache.Get": {Count: 1}},
			},
		},
		{
			name: "both_empty",
			prev: nil,
			curr: nil,
		},
	}

	g := goldie.New(t,
		goldie.WithFixtureDir(".testdata"),
		goldie.WithNameSuffix(".json"),
	)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := CouplingStats(tt.prev, tt.curr)
			g.AssertJson(t, "coupling/"+tt.name, result)
		})
	}
}

func TestDiffAllPackages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		prev []analyzer.Metrics
		curr []analyzer.Metrics
	}{
		{
			name: "new_package",
			prev: nil,
			curr: []analyzer.Metrics{
				{
					Package: "internal/cache",
					Outward: analyzer.PackageCouplingStats{
						"internal/db": analyzer.CouplingStats{
							"db.Pool": {Count: 3},
						},
					},
				},
			},
		},
		{
			name: "removed_package",
			prev: []analyzer.Metrics{
				{
					Package: "internal/oldutil",
					Outward: analyzer.PackageCouplingStats{
						"internal/core": analyzer.CouplingStats{
							"core.Helper": {Count: 1},
						},
					},
				},
			},
			curr: nil,
		},
		{
			name: "unchanged_package",
			prev: []analyzer.Metrics{
				{
					Package: "internal/core",
					Outward: analyzer.PackageCouplingStats{
						"internal/db": analyzer.CouplingStats{"db.Query": {Count: 1}},
					},
				},
			},
			curr: []analyzer.Metrics{
				{
					Package: "internal/core",
					Outward: analyzer.PackageCouplingStats{
						"internal/db": analyzer.CouplingStats{"db.Query": {Count: 1}},
					},
				},
			},
		},
		{
			name: "mixed",
			prev: []analyzer.Metrics{
				{
					Package: "internal/core",
					Outward: analyzer.PackageCouplingStats{
						"internal/config": analyzer.CouplingStats{"config.Debug": {Count: 1}},
					},
				},
				{
					Package: "internal/oldutil",
					Outward: analyzer.PackageCouplingStats{
						"internal/core": analyzer.CouplingStats{"core.Helper": {Count: 1}},
					},
				},
			},
			curr: []analyzer.Metrics{
				{
					Package: "internal/core",
					Outward: analyzer.PackageCouplingStats{
						"internal/config": analyzer.CouplingStats{"config.Timeout": {Count: 2}},
						"internal/http":   analyzer.CouplingStats{"http.Client": {Count: 1}},
					},
				},
				{
					Package: "internal/cache",
					Outward: analyzer.PackageCouplingStats{
						"internal/db": analyzer.CouplingStats{"db.Pool": {Count: 3}},
					},
				},
			},
		},
		{
			name: "both_empty",
			prev: nil,
			curr: nil,
		},
	}

	g := goldie.New(t,
		goldie.WithFixtureDir(".testdata"),
		goldie.WithNameSuffix(".json"),
	)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := AllPackages(tt.prev, tt.curr)
			g.AssertJson(t, "packages/"+tt.name, result)
		})
	}
}

func TestPositionKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		pos  analyzer.Position
		want string
	}{
		{"simple", analyzer.Position{File: "main.go", Line: 10}, "main.go:10"},
		{
			"with_path",
			analyzer.Position{File: "internal/pkg/foo.go", Line: 42},
			"internal/pkg/foo.go:42",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, positionKey(tt.pos))
		})
	}
}
