/*
Copyright © 2026 Flamingoose Software Inc <eng@flamingoose.ca>
*/
package cmd

import (
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/flamingoosesoftwareinc/uda/internal/detect"
	"github.com/flamingoosesoftwareinc/uda/internal/files"
	"github.com/flamingoosesoftwareinc/uda/internal/ts"
	"github.com/spf13/cobra"
)

// metricsCmd represents the metrics command
var metricsCmd = &cobra.Command{
	Use:   "metrics",
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		path := "."
		if len(args) == 1 {
			path = args[0]
		}

		dirFS := os.DirFS(path)
		fileList, err := files.ListFiles(ctx, dirFS, files.SkipHidden())
		if err != nil {
			return err
		}

		parsers := ts.Parsers()
		defer parsers.Close()

		for _, file := range fileList {
			lang, err := detect.Detect(ctx, dirFS, file)
			if err != nil {
				slog.ErrorContext(ctx, "error detecting language", "path", file, "error", err)
			}

			fmt.Println(file, lang)

			parser, err := parsers.LoadParser(lang)
			if err != nil {
				switch {
				case errors.Is(err, ts.ErrLangNotSupported):
					slog.WarnContext(
						ctx,
						"language not supported",
						"lang",
						lang,
						"path",
						path,
						"error",
						err,
					)
				default:
					slog.WarnContext(
						ctx,
						"error loading parser",
						"lang",
						lang,
						"path",
						path,
						"error",
						err,
					)

				}
				continue
			}

			tree, content, err := parser.ParseCtx(ctx, dirFS, file)
			if err != nil {
				slog.WarnContext(
					ctx,
					"error parsing",
					"lang",
					lang,
					"path",
					path,
					"error",
					err,
				)
				continue
			}
			if _, _, err := parser.Query(tree, content); err != nil {
				slog.WarnContext(ctx, "error querying", "lang", lang, "path", path, "error", err)
			}
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(metricsCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// metricsCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// metricsCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
