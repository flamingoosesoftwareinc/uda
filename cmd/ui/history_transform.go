package ui

import (
	"slices"
	"time"

	"github.com/flamingoosesoftwareinc/uda/internal/history"
)

// PackageDataPoint holds metrics for a single package at a single commit.
type PackageDataPoint struct {
	SHA         string
	Timestamp   time.Time
	Message     string
	Inward      int
	Outward     int
	Instability float64
}

// PackageTimeSeries holds time series data for a single package.
type PackageTimeSeries struct {
	Package      string
	Language     string
	DataPoints   []PackageDataPoint
	ChangeFreq   float64 // from hotspot analysis (0 when no hotspot data)
	HotspotScore float64 // from hotspot analysis (0 when no hotspot data)
}

// LanguageTimeSeries groups time series by language.
type LanguageTimeSeries struct {
	Language string
	Series   []PackageTimeSeries
}

// TransformToTimeSeries converts commit metrics to per-package time series.
// Returns a map of language -> []PackageTimeSeries.
func TransformToTimeSeries(commits []history.CommitMetrics) []LanguageTimeSeries {
	// Build map: language -> package -> []datapoint
	langPkgData := make(map[string]map[string][]PackageDataPoint)

	for _, commit := range commits {
		for _, langMetrics := range commit.Metrics {
			lang := langMetrics.Language
			if _, ok := langPkgData[lang]; !ok {
				langPkgData[lang] = make(map[string][]PackageDataPoint)
			}

			for _, metric := range langMetrics.Metrics {
				dataPoint := PackageDataPoint{
					SHA:         commit.SHA,
					Timestamp:   commit.Timestamp,
					Message:     commit.Message,
					Inward:      metric.Inward,
					Outward:     metric.Outward,
					Instability: metric.Instability,
				}
				langPkgData[lang][metric.Package] = append(
					langPkgData[lang][metric.Package],
					dataPoint,
				)
			}
		}
	}

	// Convert to sorted slice
	result := make([]LanguageTimeSeries, 0, len(langPkgData))

	// Sort languages
	langs := make([]string, 0, len(langPkgData))
	for lang := range langPkgData {
		langs = append(langs, lang)
	}

	slices.Sort(langs)

	for _, lang := range langs {
		pkgData := langPkgData[lang]

		// Sort packages
		pkgs := make([]string, 0, len(pkgData))
		for pkg := range pkgData {
			pkgs = append(pkgs, pkg)
		}

		slices.Sort(pkgs)

		series := make([]PackageTimeSeries, 0, len(pkgs))

		for _, pkg := range pkgs {
			dataPoints := pkgData[pkg]
			// Sort by timestamp (oldest first)
			slices.SortFunc(dataPoints, func(a, b PackageDataPoint) int {
				return a.Timestamp.Compare(b.Timestamp)
			})

			series = append(series, PackageTimeSeries{
				Package:    pkg,
				Language:   lang,
				DataPoints: dataPoints,
			})
		}

		result = append(result, LanguageTimeSeries{
			Language: lang,
			Series:   series,
		})
	}

	return result
}

// GetLanguages returns unique languages from commit metrics.
func GetLanguages(commits []history.CommitMetrics) []string {
	seen := make(map[string]struct{})

	var langs []string

	for _, commit := range commits {
		for _, langMetrics := range commit.Metrics {
			if _, ok := seen[langMetrics.Language]; !ok {
				seen[langMetrics.Language] = struct{}{}
				langs = append(langs, langMetrics.Language)
			}
		}
	}

	slices.Sort(langs)

	return langs
}

// FilterByLanguage returns time series for a specific language.
func FilterByLanguage(all []LanguageTimeSeries, lang string) []PackageTimeSeries {
	for _, lts := range all {
		if lts.Language == lang {
			return lts.Series
		}
	}

	return nil
}
