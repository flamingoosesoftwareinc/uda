package tscore

import (
	"context"
	"encoding/json"
	"io/fs"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"

	"github.com/flamingoosesoftwareinc/uda/internal/logschema"
	"muzzammil.xyz/jsonc"
)

// Directory is a filesystem directory path used as a package-boundary key.
type (
	Directory string
	// PackageName is a package.json "name" value.
	PackageName string
)

// PathAlias is a tsconfig path mapping from an import prefix to candidate targets.
type PathAlias struct {
	Prefix  string
	Targets []string
}

type packageJSONPartial struct {
	Name string `json:"name"`
}

// ExtractPackageNames reads each package.json and maps its directory to the
// declared package name, skipping files that fail to parse or lack a name.
func ExtractPackageNames(
	ctx context.Context,
	dir fs.FS,
	packageJSONPaths []string,
) (map[Directory]PackageName, error) {
	pkgNames := make(map[Directory]PackageName, len(packageJSONPaths))

	for _, pjPath := range packageJSONPaths {
		data, err := fs.ReadFile(dir, pjPath)
		if err != nil {
			return nil, err
		}

		var pkgJSON packageJSONPartial
		if err := json.Unmarshal(data, &pkgJSON); err != nil {
			slog.LogAttrs(
				ctx,
				slog.LevelDebug,
				"ExtractPackageNames: failed to parse package.json",
				logschema.UdaErrorPath(pjPath),
				logschema.UdaErrorMessage(err.Error()),
			)

			continue
		}

		if pkgJSON.Name == "" {
			slog.LogAttrs(
				ctx,
				slog.LevelDebug,
				"ExtractPackageNames: package.json has no name field",
				logschema.UdaErrorPath(pjPath),
			)

			continue
		}

		pkgNames[Directory(filepath.Dir(pjPath))] = PackageName(pkgJSON.Name)
	}

	return pkgNames, nil
}

type tsconfigPartial struct {
	Extends         string `json:"extends"`
	CompilerOptions struct {
		BaseURL string              `json:"baseUrl"`
		Paths   map[string][]string `json:"paths"`
	} `json:"compilerOptions"`
	Exclude []string `json:"exclude"`
}

// ExtractExcludePatterns returns the union of "exclude" globs across the tsconfig files.
func ExtractExcludePatterns(ctx context.Context, dir fs.FS, tsconfigPaths []string) []string {
	seen := make(map[string]struct{})

	var patterns []string

	for _, tcPath := range tsconfigPaths {
		resolved, err := resolveTsconfig(ctx, dir, tcPath)
		if err != nil {
			continue
		}

		tcDir := filepath.Dir(tcPath)
		for _, pat := range resolved.Exclude {
			full := filepath.Join(tcDir, pat)

			full = filepath.Clean(full)
			if _, ok := seen[full]; !ok {
				seen[full] = struct{}{}
				patterns = append(patterns, full)
			}
		}
	}

	return patterns
}

// ExtractPathAliases resolves each tsconfig's compilerOptions.paths into
// per-directory PathAlias entries, following "extends" chains.
func ExtractPathAliases(
	ctx context.Context,
	dir fs.FS,
	tsconfigPaths []string,
) (map[Directory][]PathAlias, error) {
	allAliases := make(map[Directory][]PathAlias, len(tsconfigPaths))

	for _, tcPath := range tsconfigPaths {
		resolved, err := resolveTsconfig(ctx, dir, tcPath)
		if err != nil {
			slog.LogAttrs(
				ctx,
				slog.LevelDebug,
				"ExtractPathAliases: failed to resolve tsconfig",
				logschema.UdaErrorPath(tcPath),
				logschema.UdaErrorMessage(err.Error()),
			)

			continue
		}

		if resolved.CompilerOptions.Paths == nil {
			continue
		}

		tcDir := filepath.Dir(tcPath)

		baseURL := resolved.CompilerOptions.BaseURL
		if baseURL == "" {
			baseURL = "."
		}

		aliases := make([]PathAlias, 0, len(resolved.CompilerOptions.Paths))
		for pattern, targets := range resolved.CompilerOptions.Paths {
			prefix := strings.TrimSuffix(pattern, "*")

			resolvedTargets := make([]string, 0, len(targets))
			for _, target := range targets {
				targetPath := strings.TrimSuffix(target, "*")
				resolved := filepath.Join(tcDir, baseURL, targetPath)
				resolvedTargets = append(resolvedTargets, resolved)
			}

			aliases = append(aliases, PathAlias{Prefix: prefix, Targets: resolvedTargets})
		}

		sort.Slice(aliases, func(i, j int) bool {
			return len(aliases[i].Prefix) > len(aliases[j].Prefix)
		})

		allAliases[Directory(tcDir)] = aliases
	}

	return allAliases, nil
}

func resolveTsconfig(ctx context.Context, dir fs.FS, tcPath string) (tsconfigPartial, error) {
	data, err := fs.ReadFile(dir, tcPath)
	if err != nil {
		return tsconfigPartial{}, err
	}

	data = jsonc.ToJSON(data)

	var parsed tsconfigPartial
	if err := json.Unmarshal(data, &parsed); err != nil {
		return tsconfigPartial{}, err
	}

	if parsed.Extends == "" {
		return parsed, nil
	}

	parentPath := filepath.Join(filepath.Dir(tcPath), parsed.Extends)
	if filepath.Ext(parentPath) == "" {
		parentPath += ".json"
	}

	parent, err := resolveTsconfig(ctx, dir, parentPath)
	if err != nil {
		slog.LogAttrs(
			ctx,
			slog.LevelDebug,
			"resolveTsconfig: failed to resolve parent",
			logschema.UdaErrorPath(parentPath),
			logschema.UdaErrorMessage(err.Error()),
		)

		return parsed, nil
	}

	if parsed.CompilerOptions.BaseURL == "" {
		parsed.CompilerOptions.BaseURL = parent.CompilerOptions.BaseURL
	}

	if parsed.CompilerOptions.Paths == nil {
		parsed.CompilerOptions.Paths = parent.CompilerOptions.Paths
	}

	if parsed.Exclude == nil {
		parsed.Exclude = parent.Exclude
	}

	return parsed, nil
}
