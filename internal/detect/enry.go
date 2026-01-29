package detect

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"path/filepath"

	enry "github.com/go-enry/go-enry/v2"
)

var ErrNoLanguageDetected = errors.New("no language detected")

func Detect(ctx context.Context, dirFS fs.FS, path string) (string, error) {
	content, err := readFileHead(dirFS, path, 8192)
	if err != nil {
		return "", ErrNoLanguageDetected
	}

	lang := enry.GetLanguage(filepath.Base(path), content)
	if lang == "" {
		return "", ErrNoLanguageDetected
	}

	return lang, nil
}

func readFileHead(dirFS fs.FS, path string, maxBytes int64) ([]byte, error) {
	f, err := dirFS.Open(path)
	if err != nil {
		return nil, err
	}

	defer func() { _ = f.Close() }()

	limitReader := io.LimitReader(f, maxBytes)
	return io.ReadAll(limitReader)
}
