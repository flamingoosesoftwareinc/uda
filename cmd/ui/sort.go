package ui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/flamingoosesoftwareinc/uda/internal/hotspot"
)

// SortField identifies a column to sort by.
type SortField int

// SortField enum: the column identifiers the metrics table can sort by.
const (
	SortPackage     SortField = iota
	SortInward      SortField = iota
	SortOutward     SortField = iota
	SortInstability SortField = iota
	SortChangeFreq  SortField = iota
	SortHotspot     SortField = iota
)

// SortDirection is ascending or descending.
type SortDirection int

// SortDirection enum: ascending or descending.
const (
	Asc  SortDirection = iota
	Desc SortDirection = iota
)

// SortCriterion pairs a field with a direction.
type SortCriterion struct {
	Field     SortField
	Direction SortDirection
}

// DefaultSort returns the default sort: instability desc, outward desc, inward desc.
func DefaultSort() []SortCriterion {
	return []SortCriterion{
		{Field: SortInstability, Direction: Desc},
		{Field: SortOutward, Direction: Desc},
		{Field: SortInward, Direction: Desc},
	}
}

var fieldNames = map[string]SortField{
	"package":       SortPackage,
	"inward":        SortInward,
	"outward":       SortOutward,
	"instability":   SortInstability,
	"changefreq":    SortChangeFreq,
	"change_freq":   SortChangeFreq,
	"change-freq":   SortChangeFreq,
	"chng_freq":     SortChangeFreq,
	"hotspot":       SortHotspot,
	"hotspotscore":  SortHotspot,
	"hotspot_score": SortHotspot,
}

var dirNames = map[string]SortDirection{
	"asc":  Asc,
	"desc": Desc,
}

// canonicalFieldNames maps each SortField to its preferred display name.
var canonicalFieldNames = map[SortField]string{
	SortPackage:     "package",
	SortInward:      "inward",
	SortOutward:     "outward",
	SortInstability: "instability",
	SortChangeFreq:  "change_freq",
	SortHotspot:     "hotspot",
}

// validFieldNames returns a sorted list of canonical field names for error messages.
func validFieldNames() string {
	names := make([]string, 0, len(canonicalFieldNames))
	for _, name := range canonicalFieldNames {
		names = append(names, name)
	}

	slices.Sort(names)

	return strings.Join(names, ", ")
}

// ParseSort parses a sort string like "package:asc,instability:desc".
func ParseSort(s string) ([]SortCriterion, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return DefaultSort(), nil
	}

	parts := strings.Split(s, ",")
	criteria := make([]SortCriterion, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)

		fieldStr, dirStr, ok := strings.Cut(part, ":")
		if !ok {
			return nil, fmt.Errorf("invalid sort criterion %q: expected field:direction", part)
		}

		field, fieldOk := fieldNames[strings.ToLower(fieldStr)]
		if !fieldOk {
			return nil, fmt.Errorf(
				"unknown sort field %q: valid fields are %s",
				fieldStr, validFieldNames(),
			)
		}

		dir, dirOk := dirNames[strings.ToLower(dirStr)]
		if !dirOk {
			return nil, fmt.Errorf(
				"unknown sort direction %q: valid directions are asc, desc",
				dirStr,
			)
		}

		criteria = append(criteria, SortCriterion{Field: field, Direction: dir})
	}

	return criteria, nil
}

// SortFunc compares two metrics, returning negative/zero/positive.
type SortFunc func(a, b analyzer.Metrics) int

// SortMetrics sorts a copy of metrics by applying sort functions in order.
// The first non-zero result wins (multi-key sort).
func SortMetrics(metrics []analyzer.Metrics, fns ...SortFunc) []analyzer.Metrics {
	sorted := make([]analyzer.Metrics, len(metrics))
	copy(sorted, metrics)

	slices.SortFunc(sorted, func(a, b analyzer.Metrics) int {
		for _, fn := range fns {
			if cmp := fn(a, b); cmp != 0 {
				return cmp
			}
		}

		return 0
	})

	return sorted
}

// ByField returns a SortFunc for a built-in field with direction.
// For SortChangeFreq/SortHotspot it returns a no-op (use ByChangeFreq/ByHotspot instead).
func ByField(field SortField, dir SortDirection) SortFunc {
	return func(left, right analyzer.Metrics) int {
		var cmp int

		switch field {
		case SortPackage:
			if left.Package < right.Package {
				cmp = -1
			} else if left.Package > right.Package {
				cmp = 1
			}
		case SortInward:
			cmp = compareFloat(left.InwardCoupling(), right.InwardCoupling())
		case SortOutward:
			cmp = compareFloat(left.OutwardCoupling(), right.OutwardCoupling())
		case SortInstability:
			cmp = compareFloat(left.Instability(), right.Instability())
		case SortChangeFreq, SortHotspot:
			// no-op — use ByChangeFreq/ByHotspot which take a scores map
			return 0
		}

		if dir == Desc {
			cmp = -cmp
		}

		return cmp
	}
}

// ByChangeFreq returns a SortFunc that sorts by change frequency.
func ByChangeFreq(scores map[string]hotspot.PackageScore, dir SortDirection) SortFunc {
	return func(a, b analyzer.Metrics) int {
		cmp := compareFloat(
			scores[string(a.Package)].ChangeFreq,
			scores[string(b.Package)].ChangeFreq,
		)
		if dir == Desc {
			cmp = -cmp
		}

		return cmp
	}
}

// ByHotspot returns a SortFunc that sorts by hotspot score.
func ByHotspot(scores map[string]hotspot.PackageScore, dir SortDirection) SortFunc {
	return func(a, b analyzer.Metrics) int {
		cmp := compareFloat(
			scores[string(a.Package)].HotspotScore,
			scores[string(b.Package)].HotspotScore,
		)
		if dir == Desc {
			cmp = -cmp
		}

		return cmp
	}
}

// BuildSortFuncs converts criteria into a SortFunc slice.
// Scores may be nil; change_freq/hotspot criteria are skipped when nil.
func BuildSortFuncs(criteria []SortCriterion, scores map[string]hotspot.PackageScore) []SortFunc {
	fns := make([]SortFunc, 0, len(criteria))
	for _, c := range criteria {
		switch c.Field {
		case SortChangeFreq:
			if scores != nil {
				fns = append(fns, ByChangeFreq(scores, c.Direction))
			}
		case SortHotspot:
			if scores != nil {
				fns = append(fns, ByHotspot(scores, c.Direction))
			}
		case SortPackage, SortInward, SortOutward, SortInstability:
			fns = append(fns, ByField(c.Field, c.Direction))
		}
	}

	return fns
}

func compareFloat(a, b float64) int {
	if a < b {
		return -1
	}

	if a > b {
		return 1
	}

	return 0
}

// ToggleSort cycles sort on a field: first press → asc, second → desc, third → reset to default.
func ToggleSort(current []SortCriterion, field SortField) []SortCriterion {
	if len(current) == 1 && current[0].Field == field {
		if current[0].Direction == Asc {
			return []SortCriterion{{Field: field, Direction: Desc}}
		}
		// Currently desc → reset to default
		return DefaultSort()
	}
	// Not currently sorted by this field alone → set to asc
	return []SortCriterion{{Field: field, Direction: Asc}}
}

// SortIndicator returns a sort direction indicator for column headers.
func SortIndicator(criteria []SortCriterion, field SortField) string {
	if len(criteria) == 1 && criteria[0].Field == field {
		if criteria[0].Direction == Asc {
			return " ▲"
		}

		return " ▼"
	}

	return ""
}
