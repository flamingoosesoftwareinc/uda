package ts

import "errors"

type LanguageID string

const (
	GO     LanguageID = "go"
	JS     LanguageID = "javascript"
	JSX    LanguageID = "jsx"
	PYTHON LanguageID = "python"
	RUST   LanguageID = "rust"
	TS     LanguageID = "typescript"
	TSX    LanguageID = "tsx"
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
