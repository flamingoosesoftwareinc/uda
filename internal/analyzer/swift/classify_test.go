package swift

import (
	"testing"

	"github.com/flamingoosesoftwareinc/uda/internal/analyzer/swift/manifest"
	"github.com/stretchr/testify/require"
)

func TestClassifyImport(t *testing.T) {
	t.Parallel()

	m := &manifest.Manifest{
		Name: "Nuke",
		Targets: []manifest.Target{
			{Name: "Nuke", IsTest: false},
			{Name: "NukeUI", IsTest: false, Dependencies: []string{"Nuke"}},
			{Name: "NukeTests", IsTest: true, Dependencies: []string{"Nuke"}},
		},
	}

	tests := map[string]struct {
		module   string
		wantKind ImportKind
	}{
		"Foundation is system": {
			module:   "Foundation",
			wantKind: ImportSystem,
		},
		"UIKit is system": {
			module:   "UIKit",
			wantKind: ImportSystem,
		},
		"CoreGraphics is system": {
			module:   "CoreGraphics",
			wantKind: ImportSystem,
		},
		"SwiftUI is system": {
			module:   "SwiftUI",
			wantKind: ImportSystem,
		},
		"Combine is system": {
			module:   "Combine",
			wantKind: ImportSystem,
		},
		"Nuke is project target": {
			module:   "Nuke",
			wantKind: ImportProject,
		},
		"NukeUI is project target": {
			module:   "NukeUI",
			wantKind: ImportProject,
		},
		"Alamofire is external": {
			module:   "Alamofire",
			wantKind: ImportExternal,
		},
		"Kingfisher is external": {
			module:   "Kingfisher",
			wantKind: ImportExternal,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := ClassifyImport(tt.module, m)
			require.Equal(t, tt.wantKind, got)
		})
	}
}
