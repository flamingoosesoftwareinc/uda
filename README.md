# uda - the universal dependency analyzer

A CLI tool for identifying refactoring candidates in code by measuring Efferent and Afferent coupling in packages and deriving an instability metric from it.

Uses tree-sitter to support analyzing any language.

## Installation

```sh 
go install github.com/flamingoosesoftwareinc/uda@latest
```


This will install to your GOBIN directory so please ensure it is on the path.

## Coupling and Stabilty metrics

- Afferent coupling (Ca) — the number of packages that depend on this package. High Ca means a lot of things break if you change it.
- Efferent coupling (Ce) — the number of packages this package depends on. High Ce means this package is volatile—changes elsewhere ripple into it.
- Instability (I) = Ce / (Ca + Ce) — ranges from 0 (stable, everyone depends on you) to 1 (unstable, you depend on everyone).

A package with high Ca and low abstraction is in the "zone of pain", it's concrete, everyone depends on it, and it's a nightmare to change. These are your refactoring candidates but still require further inspection.

These terms were defined in Robert Martin's book  [**Agile software development: principles, patterns and practices**](https://www.amazon.ca/Software-Development-Principles-Patterns-Practices/dp/0135974445)

You can also read about them on [wikipedia](https://en.wikipedia.org/wiki/Software_package_metrics)

## Why?

The last year I reviewed more code than ever. It has become difficult to keep up with the sheer volume of output with agentic coding, and this train doesn't look like it is going to slow down.

This tool helps me to at least identify areas of concern with respect to Change Amplification. Change Amplification is when making a change in one part of the application requires making changes to another. It is can be caused by tight coupling, lack of abstraction, duplicated logic, etc.

In my experience, Change Amplification risk accumulates over time and is one of the largest wastes of time. At the lowest level I'm looking for
1. Is this package going to be easy to change?
2. Is this package volatile (depending on a lot of other packages)?

This tool is designed to help identify these areas of concern.

## Will this work with AI agents?

It's a CLI tool, agnostic of any AI agent or it's interface. This tool can be used from the terminal.

Local MCP support over stdin coming soon.

## Dependencies

- [go-enry](github.com/go-enry/go-enry) - For identifying languages
- [go-tree-sitter](github.com/tree-sitter/go-tree-sitter) - Official Go tree-sitter bindings
- [cobra](github.com/spf13/cobra) - cli
- [ophis](github.com/njayp/ophis) - converts cobra CLI into an MCP server
