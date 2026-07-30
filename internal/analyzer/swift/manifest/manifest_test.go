package manifest_test

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/flamingoosesoftwareinc/uda/internal/analyzer/swift/manifest"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", os.Getenv("UPDATE_GOLDEN") == "1", "update golden files")

func TestParse_Nuke(t *testing.T) {
	t.Parallel()

	dir := os.DirFS(".testdata/nuke")
	got, err := manifest.Parse(dir)
	require.NoError(t, err)

	actual, err := json.MarshalIndent(got, "", "  ")
	require.NoError(t, err)

	goldenPath := filepath.Join(".testdata", "nuke", "golden.json")
	if *update {
		err := os.WriteFile(goldenPath, actual, 0o644)
		require.NoError(t, err)

		return
	}

	expected, err := os.ReadFile(goldenPath)
	require.NoError(t, err, "golden file not found, run with -update to generate")
	require.JSONEq(t, string(expected), string(actual))
}

func TestParse_SingleTarget(t *testing.T) {
	t.Parallel()

	fs := fstest.MapFS{
		"Package.swift": &fstest.MapFile{
			Data: []byte(`// swift-tools-version:5.9
import PackageDescription

let package = Package(
    name: "HelloWorld",
    targets: [
        .executableTarget(name: "HelloWorld"),
        .testTarget(name: "HelloWorldTests", dependencies: ["HelloWorld"]),
    ]
)
`),
		},
	}

	got, err := manifest.Parse(fs)
	require.NoError(t, err)

	require.Equal(t, "HelloWorld", got.Name)
	require.Len(t, got.Targets, 2)

	prod := got.Targets[0]
	require.Equal(t, "HelloWorld", prod.Name)
	require.False(t, prod.IsTest)

	test := got.Targets[1]
	require.Equal(t, "HelloWorldTests", test.Name)
	require.True(t, test.IsTest)
	require.Contains(t, test.Dependencies, "HelloWorld")
}

func TestParse_NestedProductDeps(t *testing.T) {
	t.Parallel()

	dir := os.DirFS(".testdata/nested_deps")
	got, err := manifest.Parse(dir)
	require.NoError(t, err)

	require.Equal(t, "NestedDeps", got.Name)
	require.Len(t, got.Targets, 2, "expected 2 targets: NestedDeps and NestedDepsTests")

	lib := got.Targets[0]
	require.Equal(t, "NestedDeps", lib.Name)
	require.False(t, lib.IsTest)
	require.Equal(t, []string{"Bar"}, lib.Dependencies,
		"should extract 'Bar' from .product(name: \"Bar\", package: \"Baz\")")

	test := got.Targets[1]
	require.Equal(t, "NestedDepsTests", test.Name)
	require.True(t, test.IsTest)
	require.Equal(t, []string{"NestedDeps"}, test.Dependencies)
}

func TestParse_NoPackageSwift(t *testing.T) {
	t.Parallel()

	fs := fstest.MapFS{
		"main.swift": &fstest.MapFile{Data: []byte("print(\"hello\")\n")},
	}

	_, err := manifest.Parse(fs)
	require.Error(t, err)
}
