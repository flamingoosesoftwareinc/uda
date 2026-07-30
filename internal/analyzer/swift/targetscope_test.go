package swift_test

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/flamingoosesoftwareinc/uda/internal/analyzer/swift"
	"github.com/stretchr/testify/require"
)

func TestAnalyze_TargetScoped_NoHelperPackage(t *testing.T) {
	// When analyzing with a manifest, directories that are not SPM targets
	// (like Tests/Helpers, Tests/Mocks) must be folded into their parent
	// test target — they should NOT appear as independent packages.
	t.Parallel()

	fs := fstest.MapFS{
		"Package.swift": &fstest.MapFile{
			Data: []byte(`// swift-tools-version:5.9
import PackageDescription

let package = Package(
    name: "MyLib",
    targets: [
        .target(name: "MyLib"),
        .testTarget(name: "MyLibTests", dependencies: ["MyLib"]),
    ]
)
`),
		},
		"Sources/MyLib/lib.swift": &fstest.MapFile{
			Data: []byte("import Foundation\n\npublic struct MyLib {}\n"),
		},
		"Tests/MyLibTests/test.swift": &fstest.MapFile{
			Data: []byte("import MyLib\n\nfinal class MyLibTests {}\n"),
		},
		"Tests/MyLibTests/Helpers/helper.swift": &fstest.MapFile{
			Data: []byte("struct TestHelper {}\n"),
		},
	}

	a := swift.SwiftAnalyzer()
	got, err := a.Analyze(context.Background(), fs)
	require.NoError(t, err)

	packageNames := make(map[string]bool, len(got))
	for _, m := range got {
		packageNames[string(m.Package)] = true
	}

	require.True(t, packageNames["MyLib"], "expected MyLib in metrics")
	require.True(t, packageNames["MyLibTests"], "expected MyLibTests in metrics")
	require.False(
		t,
		packageNames["Helpers"],
		"Helpers should be folded into MyLibTests, not a separate package",
	)
}

func TestAnalyze_TargetScoped_PeerHelperDirs(t *testing.T) {
	// When Tests/ contains peer directories that are NOT declared SPM targets
	// (e.g., Tests/Helpers/, Tests/Mocks/), they should NOT appear as
	// independent packages. This mirrors real-world layouts like Nuke where
	// helper directories sit alongside test targets, not nested under them.
	t.Parallel()

	fs := fstest.MapFS{
		"Package.swift": &fstest.MapFile{
			Data: []byte(`// swift-tools-version:5.9
import PackageDescription

let package = Package(
    name: "MyLib",
    targets: [
        .target(name: "MyLib"),
        .testTarget(name: "MyLibTests", dependencies: ["MyLib"]),
    ]
)
`),
		},
		"Sources/MyLib/lib.swift": &fstest.MapFile{
			Data: []byte("import Foundation\n\npublic struct MyLib {}\n"),
		},
		"Tests/MyLibTests/test.swift": &fstest.MapFile{
			Data: []byte("import MyLib\n\nfinal class MyLibTests {}\n"),
		},
		"Tests/Helpers/helper.swift": &fstest.MapFile{
			Data: []byte("struct TestHelper {}\n"),
		},
		"Tests/Mocks/mock.swift": &fstest.MapFile{
			Data: []byte("struct MockService {}\n"),
		},
	}

	a := swift.SwiftAnalyzer()
	got, err := a.Analyze(context.Background(), fs)
	require.NoError(t, err)

	packageNames := make(map[string]bool, len(got))
	for _, m := range got {
		packageNames[string(m.Package)] = true
	}

	require.True(t, packageNames["MyLib"], "expected MyLib in metrics")
	require.True(t, packageNames["MyLibTests"], "expected MyLibTests in metrics")
	require.False(
		t,
		packageNames["Helpers"],
		"Helpers is not a declared target — should be folded, not a separate package",
	)
	require.False(
		t,
		packageNames["Mocks"],
		"Mocks is not a declared target — should be folded, not a separate package",
	)
}
