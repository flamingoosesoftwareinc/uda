package evocoupling

import "sort"

// Advisory flags a co-change expectation a change set did not meet: the
// touched package historically changes together with the expected one,
// and this time it did not. Purely informational — the review command
// renders advisories without affecting its exit code.
type Advisory struct {
	Touched     string  `json:"touched"`
	Expected    string  `json:"expected"`
	Correlation float64 `json:"correlation"`
}

// AdviseOptions tunes the advisory pass.
type AdviseOptions struct {
	// MinCorrelation drops pairs below the threshold. Zero keeps every
	// pair the coupling analysis surfaced.
	MinCorrelation float64

	// IncludeLowSupport keeps pairs flagged LowSupport. Off by default:
	// young or sparse histories mark almost every pair low-support, and
	// advisories built on them are noise.
	IncludeLowSupport bool
}

// Advise compares a co-change model (pairs from Analyze) against the set
// of packages a change touched. For each qualifying pair with exactly one
// side touched, it emits an advisory for the untouched side. Output is
// sorted by correlation (descending), then by touched/expected name.
func Advise(pairs []CouplingPair, touched map[string]bool, opts AdviseOptions) []Advisory {
	var advisories []Advisory

	for _, pair := range pairs {
		if pair.LowSupport && !opts.IncludeLowSupport {
			continue
		}

		if pair.Correlation < opts.MinCorrelation {
			continue
		}

		touchedA, touchedB := touched[pair.PackageA], touched[pair.PackageB]

		if touchedA && !touchedB {
			advisories = append(advisories, Advisory{
				Touched:     pair.PackageA,
				Expected:    pair.PackageB,
				Correlation: pair.Correlation,
			})
		}

		if touchedB && !touchedA {
			advisories = append(advisories, Advisory{
				Touched:     pair.PackageB,
				Expected:    pair.PackageA,
				Correlation: pair.Correlation,
			})
		}
	}

	sort.Slice(advisories, func(i, j int) bool {
		if advisories[i].Correlation != advisories[j].Correlation {
			return advisories[i].Correlation > advisories[j].Correlation
		}

		if advisories[i].Touched != advisories[j].Touched {
			return advisories[i].Touched < advisories[j].Touched
		}

		return advisories[i].Expected < advisories[j].Expected
	})

	return advisories
}
