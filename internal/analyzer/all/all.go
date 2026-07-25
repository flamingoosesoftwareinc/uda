// Package all aggregates every supported language analyzer for one-shot analysis.
package all

import (
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer/golang"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer/python"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer/rust"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer/swift"
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer/typescript"
)

// Analyzers returns every analyzer with default options.
// This is the single explicit list — adding an analyzer here
// makes it available to the CLI and test infrastructure.
func Analyzers() []analyzer.Analyzer {
	return []analyzer.Analyzer{
		golang.GoAnalyzer(),
		python.PythonAnalyzer(),
		rust.RustAnalyzer(),
		swift.SwiftAnalyzer(),
		typescript.TsAnalyzer(),
	}
}
