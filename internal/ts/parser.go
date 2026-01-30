package ts

import (
	"errors"
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

type LanguageID string

const (
	GO     LanguageID = "go"
	JS     LanguageID = "javascript"
	JSX    LanguageID = "jsx"
	PYTHON LanguageID = "python"
	RUST   LanguageID = "rust"
	TS                = "typescript"
	TSX               = "tsx"
)

var langs = map[LanguageID]struct{}{
	GO:     {},
	JS:     {},
	JSX:    {},
	PYTHON: {},
	RUST:   {},
	TS:     {},
	TSX:    {},
}

func isLangSupported(lang LanguageID) bool {
	_, ok := langs[lang]
	return ok
}

var ErrLangNotSupported = errors.New("language not supported")

type parser struct {
	sync.Mutex
	parser *treesitter.Parser
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
		lerr := prsr.SetLanguage(treesitter.NewLanguage(tsgo.Language()))
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

		lerr := prsr.SetLanguage(treesitter.NewLanguage(tsjavascript.Language()))
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

		lerr := prsr.SetLanguage(treesitter.NewLanguage(tsjavascript.Language()))
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

		lerr := prsr.SetLanguage(treesitter.NewLanguage(tspython.Language()))
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

		lerr := prsr.SetLanguage(treesitter.NewLanguage(tsrust.Language()))
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

		lerr := prsr.SetLanguage(treesitter.NewLanguage(tstypescript.LanguageTypescript()))
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

		lerr := prsr.SetLanguage(treesitter.NewLanguage(tstypescript.LanguageTSX()))
		err = lerr
		if err != nil {
			return
		}

		p.tsxParser = &parser{parser: prsr}
	})

	return p.tsxParser, err
}
