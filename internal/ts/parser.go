package ts

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"sync"

	treesitter "github.com/tree-sitter/go-tree-sitter"
)

type parser struct {
	sync.Mutex
	query    string
	language *treesitter.Language
	parser   *treesitter.Parser
}

func (p *parser) ParseCtx(
	ctx context.Context,
	dirFS fs.FS,
	path string,
) (*treesitter.Tree, []byte, error) {
	p.Lock()
	defer p.Unlock()

	f, err := dirFS.Open(path)
	if err != nil {
		return nil, nil, err
	}

	text, err := io.ReadAll(f)
	if err != nil {
		return nil, nil, err
	}

	length := len(text)
	return p.parser.ParseWithOptions(
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

func (p *parser) Query(
	tree *treesitter.Tree,
	content []byte,
) (*treesitter.Query, *treesitter.QueryCursor, error) {
	p.Lock()
	defer p.Unlock()

	q, err := treesitter.NewQuery(p.language, p.query)
	if err != nil {
		return nil, nil, err
	}

	captureNames := q.CaptureNames()

	qc := treesitter.NewQueryCursor()
	matches := qc.Matches(q, tree.RootNode(), content)
	match := matches.Next()
	for match != nil {
		for _, capture := range match.Captures {
			node := capture.Node
			captureName := captureNames[capture.Index]
			fmt.Printf("%s: %s\n", captureName, string(node.Utf8Text(content)))
		}
		match = matches.Next()
	}

	return q, qc, nil
}

func (p *parser) Close() {
	p.Lock()
	defer p.Unlock()
	slog.Info("closing parser")
	if p.parser == nil {
		return
	}

	p.parser.Close()
}
