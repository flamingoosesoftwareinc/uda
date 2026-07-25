// Package swift implements the Swift source analyzer (Package.swift target + import tracking).
package swift

// systemFrameworks (frameworks_generated.go) is refreshed manually, not via
// `go generate`: gen-frameworks fetches live data from Apple's technologies.json
// and can pull upstream slug regressions into an unrelated commit, so
// regeneration must be a deliberate act. Run:
//
//	(cd internal/analyzer/swift && go run ./cmd/gen-frameworks)

import (
	"github.com/flamingoosesoftwareinc/uda/internal/analyzer/swift/manifest"
)

// ImportKind classifies a Swift import statement.
type ImportKind int

// Import kinds classify where a Swift import resolves.
const (
	ImportSystem   ImportKind = iota // Apple/Swift SDK framework
	ImportProject                    // target within the same manifest
	ImportExternal                   // third-party dependency
)

// compilerModules are implicit or compiler-provided modules that do not appear
// in Apple's technologies.json. Hand-maintained — these rarely change.
var compilerModules = map[string]struct{}{
	"Darwin":            {},
	"os":                {},
	"Glibc":             {},
	"CRT":               {},
	"XCTest":            {},
	"Testing":           {},
	"Builtin":           {},
	"Swift":             {},
	"_Concurrency":      {},
	"_StringProcessing": {},
}

// ClassifyImport returns the kind of a Swift import given the module name
// and the parsed manifest.
func ClassifyImport(module string, parsedManifest *manifest.Manifest) ImportKind {
	_, isSys := systemFrameworks[module]

	_, isComp := compilerModules[module]
	if isSys || isComp {
		return ImportSystem
	}

	if parsedManifest != nil {
		for _, t := range parsedManifest.Targets {
			if t.Name == module {
				return ImportProject
			}
		}
	}

	return ImportExternal
}
