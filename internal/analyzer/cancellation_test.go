package analyzer_test

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/flamingoosesoftwareinc/uda/internal/analyzer/all"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// minimalFS returns a minimal fstest.MapFS with enough content for the given
// analyzer to enter its file-walking loop before honouring cancellation.
func minimalFS(t *testing.T, name string) fstest.MapFS {
	t.Helper()

	switch strings.ToLower(name) {
	case "go":
		return fstest.MapFS{
			"go.mod":  {Data: []byte("module example.com/cancel\n\ngo 1.21\n")},
			"main.go": {Data: []byte("package main\n")},
		}
	case "python":
		return fstest.MapFS{
			"pyproject.toml": {Data: []byte("[project]\nname = \"cancel\"\nversion = \"0.1.0\"\n")},
			"main.py":        {Data: []byte("import os\n")},
		}
	case "rust":
		return fstest.MapFS{
			"Cargo.toml": {
				Data: []byte(
					"[package]\nname = \"cancel\"\nversion = \"0.1.0\"\nedition = \"2021\"\n",
				),
			},
			"src/main.rs": {Data: []byte("fn main() {}\n")},
		}
	case "swift":
		return fstest.MapFS{
			"Package.swift": {Data: []byte(
				"// swift-tools-version:5.5\nimport PackageDescription\n\nlet package = Package(\n    name: \"Cancel\",\n    targets: [\n        .executableTarget(name: \"Cancel\"),\n    ]\n)\n",
			)},
			"Sources/Cancel/main.swift": {Data: []byte("import Foundation\n")},
		}
	case "typescript":
		return fstest.MapFS{
			"package.json": {Data: []byte("{\"name\": \"cancel\"}\n")},
			"src/index.ts": {Data: []byte("export const x = 1;\n")},
		}
	default:
		t.Fatalf("minimalFS: no fixture defined for analyzer %q", name)

		return nil
	}
}

func TestAnalyze_HonoursCancellation(t *testing.T) {
	analyzers := all.Analyzers()
	require.NotEmpty(t, analyzers, "Analyzers() returned no analyzers")

	for _, a := range analyzers {
		t.Run(a.Name(), func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			dir := minimalFS(t, a.Name())
			_, err := a.Analyze(ctx, dir)

			assert.ErrorIs(t, err, ctx.Err(),
				"analyzer %q: expected %v on pre-cancelled context, got %v",
				a.Name(), ctx.Err(), err,
			)
		})
	}
}
