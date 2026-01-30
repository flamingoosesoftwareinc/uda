package ts

import (
	"context"
	"io"
	"io/fs"
	"log/slog"
	"strings"
	"sync"

	treesitter "github.com/tree-sitter/go-tree-sitter"
	tsgo "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tsjavascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	tspython "github.com/tree-sitter/tree-sitter-python/bindings/go"
	tsrust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
	tstypescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

type parser struct {
	sync.Mutex
	query    string
	language *treesitter.Language
	parser   *treesitter.Parser
}

func (p *parser) ParseCtx(ctx context.Context, dirFS fs.FS, path string) (*treesitter.Tree, error) {
	p.Lock()
	defer p.Unlock()

	f, err := dirFS.Open(path)
	if err != nil {
		return nil, err
	}

	text, err := io.ReadAll(f)
	if err != nil {
		return nil, err
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
	), nil
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

	qc := treesitter.NewQueryCursor()
	// qc.Matches(q, tree.RootNode(), content)

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

type parsers struct {
	golang,
	js,
	jsx,
	py,
	rs,
	ts,
	tsx sync.Once

	goParser,
	jsParser,
	jsxParser,
	pyParser,
	rsParser,
	tsParser,
	tsxParser *parser
}

func Parsers() *parsers {
	return &parsers{}
}

func (p *parsers) LoadParser(language string) (*parser, error) {
	lang := LanguageID(strings.ToLower(language))
	if !isLangSupported(lang) {
		return nil, ErrLangNotSupported
	}

	switch lang {
	case GO:
		return p.goParserFunc()
	case JS:
		return p.jsParserFunc()
	case JSX:
		return p.jsxParserFunc()
	case PYTHON:
		return p.pyParserFunc()
	case RUST:
		return p.rsParserFunc()
	case TS:
		return p.tsParserFunc()
	case TSX:
		return p.tsxParserFunc()
	}

	return nil, ErrLangNotSupported
}

func (p *parsers) Close() {
	if p.goParser != nil {
		p.goParser.Close()
	}

	if p.jsParser != nil {
		p.jsParser.Close()
	}
	if p.jsxParser != nil {
		p.jsxParser.Close()
	}
	if p.jsxParser != nil {
		p.pyParser.Close()
	}
	if p.rsParser != nil {
		p.rsParser.Close()
	}
	if p.tsParser != nil {
		p.tsParser.Close()
	}
	if p.tsxParser != nil {
		p.tsxParser.Close()
	}
}

func (p *parsers) goParserFunc() (*parser, error) {
	var err error
	p.golang.Do(func() {
		prsr := treesitter.NewParser()
		lang := treesitter.NewLanguage(tsgo.Language())
		lerr := prsr.SetLanguage(lang)
		err = lerr
		if err != nil {
			return
		}

		p.goParser = &parser{parser: prsr}
	})

	return p.goParser, err
}

func (p *parsers) jsParserFunc() (*parser, error) {
	var err error
	p.js.Do(func() {
		prsr := treesitter.NewParser()
		lang := treesitter.NewLanguage(tsjavascript.Language())
		lerr := prsr.SetLanguage(lang)
		err = lerr
		if err != nil {
			return
		}

		p.jsParser = &parser{parser: prsr}
	})

	return p.jsParser, err
}

func (p *parsers) jsxParserFunc() (*parser, error) {
	var err error
	p.jsx.Do(func() {
		prsr := treesitter.NewParser()
		lang := treesitter.NewLanguage(tsjavascript.Language())
		lerr := prsr.SetLanguage(lang)
		err = lerr
		if err != nil {
			return
		}

		p.jsxParser = &parser{parser: prsr}
	})

	return p.jsxParser, err
}

func (p *parsers) pyParserFunc() (*parser, error) {
	var err error
	p.py.Do(func() {
		prsr := treesitter.NewParser()
		lang := treesitter.NewLanguage(tspython.Language())
		lerr := prsr.SetLanguage(lang)
		err = lerr
		if err != nil {
			return
		}

		p.pyParser = &parser{parser: prsr}
	})

	return p.pyParser, err
}

func (p *parsers) rsParserFunc() (*parser, error) {
	var err error
	p.rs.Do(func() {
		prsr := treesitter.NewParser()
		lang := treesitter.NewLanguage(tsrust.Language())
		lerr := prsr.SetLanguage(lang)
		err = lerr
		if err != nil {
			return
		}

		p.rsParser = &parser{parser: prsr}
	})

	return p.rsParser, err
}

func (p *parsers) tsParserFunc() (*parser, error) {
	var err error
	p.ts.Do(func() {
		prsr := treesitter.NewParser()
		lang := treesitter.NewLanguage(tstypescript.LanguageTypescript())
		lerr := prsr.SetLanguage(lang)
		err = lerr
		if err != nil {
			return
		}

		p.tsParser = &parser{parser: prsr}
	})

	return p.tsParser, err
}

func (p *parsers) tsxParserFunc() (*parser, error) {
	var err error
	p.tsx.Do(func() {
		prsr := treesitter.NewParser()
		lang := treesitter.NewLanguage(tstypescript.LanguageTSX())
		lerr := prsr.SetLanguage(lang)
		err = lerr
		if err != nil {
			return
		}

		p.tsxParser = &parser{parser: prsr}
	})

	return p.tsxParser, err
}
