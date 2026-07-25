package ts

import (
	"context"
	"log/slog"

	treesitter "github.com/tree-sitter/go-tree-sitter"
)

// Query compiles query for language and returns the compiled query together
// with a fresh query cursor.
func Query(
	_ context.Context,
	_ *treesitter.Parser,
	language *treesitter.Language,
	_ *treesitter.Tree,
	_ []byte,
	query string,
) (*treesitter.Query, *treesitter.QueryCursor, error) {
	compiledQuery, err := treesitter.NewQuery(language, query)
	if err != nil {
		return nil, nil, err
	}

	queryCursor := treesitter.NewQueryCursor()

	return compiledQuery, queryCursor, nil
}

// CapturePair is a single tree-sitter capture: its name, text, and source span.
type CapturePair struct {
	CaptureName string
	NodeStr     string
	StartRow    uint
	StartCol    uint
	EndRow      uint
	EndCol      uint
}

// ProcessMatches flattens all matches into a single slice of CapturePair.
func ProcessMatches(
	ctx context.Context,
	matches treesitter.QueryMatches,
	captureNames []string,
	text []byte,
) []CapturePair {
	const initialCap = 256 // typical per-file capture count (avoids early regrow on small files).

	capturePairs := make([]CapturePair, 0, initialCap)

	for match := matches.Next(); match != nil; match = matches.Next() {
		pairs := ProcessCaptures(ctx, match, captureNames, text)
		capturePairs = append(capturePairs, pairs...)
	}

	return capturePairs
}

// ProcessCaptures converts a single match's captures into CapturePairs.
func ProcessCaptures(
	ctx context.Context,
	match *treesitter.QueryMatch,
	captureNames []string,
	text []byte,
) []CapturePair {
	pairs := make([]CapturePair, 0, len(match.Captures))
	for _, capture := range match.Captures {
		node := capture.Node
		nodeStr := node.Utf8Text(text)
		captureName := captureNames[capture.Index]

		startPos := node.StartPosition()
		endPos := node.EndPosition()
		pairs = append(pairs, CapturePair{
			CaptureName: captureName,
			NodeStr:     nodeStr,
			StartRow:    startPos.Row,
			StartCol:    startPos.Column,
			EndRow:      endPos.Row,
			EndCol:      endPos.Column,
		})
	}

	slog.DebugContext(ctx, "processCaptures", "captures", pairs)

	return pairs
}
