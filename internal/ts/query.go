package ts

import (
	"context"

	treesitter "github.com/tree-sitter/go-tree-sitter"
)

func Query(
	ctx context.Context,
	parser *treesitter.Parser,
	language *treesitter.Language,
	tree *treesitter.Tree,
	text []byte,
	query string,
) (*treesitter.Query, *treesitter.QueryCursor, error) {
	q, err := treesitter.NewQuery(language, query)
	if err != nil {
		return nil, nil, err
	}

	qc := treesitter.NewQueryCursor()
	return q, qc, nil
}
