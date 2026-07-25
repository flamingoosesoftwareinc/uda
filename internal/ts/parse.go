// Package ts is the shared tree-sitter parse + capture helper used by per-language analyzers.
package ts

import (
	"context"
	"io"
	"io/fs"

	treesitter "github.com/tree-sitter/go-tree-sitter"
)

// Parse reads path from dirFS and parses it with psr, returning the syntax tree
// and the file contents.
func Parse(
	ctx context.Context,
	psr *treesitter.Parser,
	dirFS fs.FS,
	path string,
) (*treesitter.Tree, []byte, error) {
	f, err := dirFS.Open(path)
	if err != nil {
		return nil, nil, err
	}

	defer func() { _ = f.Close() }()

	text, err := io.ReadAll(f)
	if err != nil {
		return nil, nil, err
	}

	length := len(text)

	return psr.ParseWithOptions(
		func(i int, _ treesitter.Point) []byte {
			if i < length {
				return text[i:]
			}

			return []byte{}
		},
		nil,
		&treesitter.ParseOptions{
			ProgressCallback: func(_ treesitter.ParseState) bool {
				select {
				case <-ctx.Done():
					return true
				default:
					return false
				}
			},
		},
	), text, nil
}
