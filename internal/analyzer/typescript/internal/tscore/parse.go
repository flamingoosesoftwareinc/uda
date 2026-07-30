package tscore

import (
	"path/filepath"
	"sync"
	"unsafe"

	"github.com/flamingoosesoftwareinc/uda/internal/analyzer"
	treesitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// CapturedImport is an import parsed from a TypeScript source file.
type CapturedImport struct {
	Name   string
	Alias  string
	Source string
	Pos    analyzer.Position
}

// IdentifierUsage is a use site of an imported identifier.
type IdentifierUsage struct {
	Name string
	Pos  analyzer.Position
}

// MemberExpr is a member access (object.property) parsed from a source file.
type MemberExpr struct {
	Object   string
	Property string
	Pos      analyzer.Position
}

// Captures holds all imports, member expressions, and identifier usages for one file.
type Captures struct {
	FilePkgPath string
	Imports     []CapturedImport
	MemberExprs []MemberExpr
	IdentUsages []IdentifierUsage
}

type captureInfo struct {
	text     string
	startRow uint
	startCol uint
	endRow   uint
	endCol   uint
}

// TsQueryBase is the tree-sitter query matching TypeScript imports and usages.
var TsQueryBase = `
(import_statement
  (import_clause (named_imports (import_specifier name: (identifier) @import_name !alias)))
  source: (string (string_fragment) @import_source))

(import_statement
  (import_clause (named_imports (import_specifier
    name: (identifier) @import_original_name
    alias: (identifier) @import_alias)))
  source: (string (string_fragment) @import_source))

(import_statement
  (import_clause (identifier) @default_import)
  source: (string (string_fragment) @import_source))

(import_statement
  (import_clause (namespace_import (identifier) @namespace_alias))
  source: (string (string_fragment) @import_source))

(export_statement
  (export_clause (export_specifier name: (identifier) @reexport_name))
  source: (string (string_fragment) @reexport_source))

(member_expression
  object: (identifier) @member_object
  property: (property_identifier) @member_property)

(call_expression
  function: (identifier) @direct_call)

(new_expression
  constructor: (identifier) @new_constructor)

(type_identifier) @type_usage
`

// TsxQueryExtra adds JSX element captures for .tsx files.
var TsxQueryExtra = `
(jsx_self_closing_element
  name: (identifier) @jsx_element)

(jsx_opening_element
  name: (identifier) @jsx_element)
`

var (
	compiledTsQuery  *treesitter.Query
	compiledTsxQuery *treesitter.Query
	tsQueryOnce      sync.Once
	errTsQuery       error
)

func initCompiledQueries() {
	tsQueryOnce.Do(func() {
		tsLang := treesitter.NewLanguage(tree_sitter_typescript.LanguageTypescript())

		var qErr *treesitter.QueryError

		compiledTsQuery, qErr = treesitter.NewQuery(tsLang, TsQueryBase)
		if qErr != nil {
			errTsQuery = qErr

			return
		}

		tsxLang := treesitter.NewLanguage(tree_sitter_typescript.LanguageTSX())

		compiledTsxQuery, qErr = treesitter.NewQuery(tsxLang, TsQueryBase+TsxQueryExtra)
		if qErr != nil {
			errTsQuery = qErr
		}
	})
}

// GetCompiledQuery returns the compiled tree-sitter query for filePath's language.
func GetCompiledQuery(filePath string) (*treesitter.Query, error) {
	initCompiledQueries()

	if filepath.Ext(filePath) == ExtTSX {
		return compiledTsxQuery, errTsQuery
	}

	return compiledTsQuery, errTsQuery
}

// LanguageForFile returns the tree-sitter language pointer for filePath's extension.
func LanguageForFile(filePath string) unsafe.Pointer {
	if filepath.Ext(filePath) == ExtTSX {
		return tree_sitter_typescript.LanguageTSX()
	}

	return tree_sitter_typescript.LanguageTypescript()
}

// GetCapturesFromMatches runs the compiled query over a parsed tree and collects
// imports, member expressions, and identifier usages.
func GetCapturesFromMatches(
	filePkgPath string,
	filePath string,
	query *treesitter.Query,
	queryCursor *treesitter.QueryCursor,
	tree *treesitter.Tree,
	text []byte,
) Captures {
	caps := Captures{FilePkgPath: filePkgPath}

	captureNames := query.CaptureNames()
	matches := queryCursor.Matches(query, tree.RootNode(), text)

	for match := matches.Next(); match != nil; match = matches.Next() {
		captureMap := make(map[string]captureInfo)

		for _, capture := range match.Captures {
			name := captureNames[capture.Index]
			startPos := capture.Node.StartPosition()
			endPos := capture.Node.EndPosition()
			captureMap[name] = captureInfo{
				text:     capture.Node.Utf8Text(text),
				startRow: startPos.Row,
				startCol: startPos.Column,
				endRow:   endPos.Row,
				endCol:   endPos.Column,
			}
		}

		appendTsImports(&caps, captureMap, filePath)
		appendTsUsages(&caps, captureMap, filePath)
	}

	return caps
}

// capturePosition converts a tree-sitter captureInfo into a 1-based analyzer.Position.
func capturePosition(filePath string, ci captureInfo) analyzer.Position {
	return analyzer.Position{
		File:     filePath,
		Line:     ci.startRow + 1,
		ColStart: ci.startCol + 1,
		ColEnd:   ci.endCol + 1,
	}
}

// appendTsImports records import statements (named, aliased, default, namespace,
// side-effect, and re-export forms) from a single query match.
func appendTsImports(caps *Captures, captureMap map[string]captureInfo, filePath string) {
	if origInfo, ok := captureMap["import_original_name"]; ok {
		if aliasInfo, ok := captureMap["import_alias"]; ok {
			if _, ok := captureMap["import_source"]; ok {
				caps.Imports = append(caps.Imports, CapturedImport{
					Name:   origInfo.text,
					Alias:  aliasInfo.text,
					Source: captureMap["import_source"].text,
					Pos:    capturePosition(filePath, aliasInfo),
				})
			}
		}
	}

	if nameInfo, ok := captureMap["import_name"]; ok {
		if _, ok := captureMap["import_source"]; ok {
			caps.Imports = append(caps.Imports, CapturedImport{
				Name:   nameInfo.text,
				Source: captureMap["import_source"].text,
				Pos:    capturePosition(filePath, nameInfo),
			})
		}
	}

	if aliasInfo, ok := captureMap["default_import"]; ok {
		if _, ok := captureMap["import_source"]; ok {
			caps.Imports = append(caps.Imports, CapturedImport{
				Name:   "default",
				Alias:  aliasInfo.text,
				Source: captureMap["import_source"].text,
				Pos:    capturePosition(filePath, aliasInfo),
			})
		}
	}

	if aliasInfo, ok := captureMap["namespace_alias"]; ok {
		if _, ok := captureMap["import_source"]; ok {
			caps.Imports = append(caps.Imports, CapturedImport{
				Name:   "*",
				Alias:  aliasInfo.text,
				Source: captureMap["import_source"].text,
				Pos:    capturePosition(filePath, aliasInfo),
			})
		}
	}

	if sourceInfo, ok := captureMap["side_effect_source"]; ok {
		_, hasNamed := captureMap["import_name"]
		_, hasDefault := captureMap["default_import"]
		_, hasNs := captureMap["namespace_alias"]

		if !hasNamed && !hasDefault && !hasNs {
			caps.Imports = append(caps.Imports, CapturedImport{
				Source: sourceInfo.text,
				Pos:    capturePosition(filePath, sourceInfo),
			})
		}
	}

	if nameInfo, ok := captureMap["reexport_name"]; ok {
		if _, ok := captureMap["reexport_source"]; ok {
			caps.Imports = append(caps.Imports, CapturedImport{
				Name:   nameInfo.text,
				Source: captureMap["reexport_source"].text,
				Pos:    capturePosition(filePath, nameInfo),
			})
		}
	}
}

// appendTsUsages records member expressions and identifier usages (calls,
// constructors, type references, JSX elements) from a single query match.
func appendTsUsages(caps *Captures, captureMap map[string]captureInfo, filePath string) {
	if objInfo, ok := captureMap["member_object"]; ok {
		if propInfo, ok := captureMap["member_property"]; ok {
			caps.MemberExprs = append(caps.MemberExprs, MemberExpr{
				Object:   objInfo.text,
				Property: propInfo.text,
				Pos:      capturePosition(filePath, propInfo),
			})
		}
	}

	if callInfo, ok := captureMap["direct_call"]; ok {
		caps.IdentUsages = append(caps.IdentUsages, IdentifierUsage{
			Name: callInfo.text,
			Pos:  capturePosition(filePath, callInfo),
		})
	}

	if ctorInfo, ok := captureMap["new_constructor"]; ok {
		caps.IdentUsages = append(caps.IdentUsages, IdentifierUsage{
			Name: ctorInfo.text,
			Pos:  capturePosition(filePath, ctorInfo),
		})
	}

	if typeInfo, ok := captureMap["type_usage"]; ok {
		caps.IdentUsages = append(caps.IdentUsages, IdentifierUsage{
			Name: typeInfo.text,
			Pos:  capturePosition(filePath, typeInfo),
		})
	}

	if jsxInfo, ok := captureMap["jsx_element"]; ok {
		caps.IdentUsages = append(caps.IdentUsages, IdentifierUsage{
			Name: jsxInfo.text,
			Pos:  capturePosition(filePath, jsxInfo),
		})
	}
}
