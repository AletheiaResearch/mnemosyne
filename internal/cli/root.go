package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/AletheiaResearch/mnemosyne/internal/config"
	"github.com/AletheiaResearch/mnemosyne/internal/tui"
	"github.com/AletheiaResearch/mnemosyne/internal/version"
)

type runtime struct {
	configPath string
	verbose    bool
	logger     *slog.Logger
	stdout     io.Writer
	stderr     io.Writer
}

func Execute(ctx context.Context) error {
	rt := &runtime{
		stdout: os.Stdout,
		stderr: os.Stderr,
	}
	root := newRootCommand(rt)
	return root.ExecuteContext(ctx)
}

func newRootCommand(rt *runtime) *cobra.Command {
	use := "mnemosyne"
	if filepath.Base(os.Args[0]) == "mem" {
		use = "mem"
	}

	cmd := &cobra.Command{
		Use:           use,
		Short:         "Export coding-assistant histories into anonymized archives",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version.Version,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			rt.logger = newLogger(rt.verbose)
			if rt.configPath == "" {
				path, err := config.File()
				if err != nil {
					return err
				}
				rt.configPath = path
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			model := tui.NewApp()
			_, err := tea.NewProgram(model, tea.WithAltScreen()).Run()
			return err
		},
	}

	cmd.SetOut(rt.stdout)
	cmd.SetErr(rt.stderr)
	cmd.PersistentFlags().StringVar(&rt.configPath, "config", "", "path to the settings file")
	cmd.PersistentFlags().BoolVar(&rt.verbose, "verbose", false, "enable debug logging")

	cmd.AddCommand(
		newSurveyCommand(rt),
		newInspectCommand(rt),
		newConfigureCommand(rt),
		newExtractCommand(rt),
		newAttestCommand(rt),
		newPublishCommand(rt),
		newTransformCommand(rt),
		newValidateCommand(rt),
		newRunlogCommand(rt),
		newSerializersCommand(),
	)
	return cmd
}

func newLogger(verbose bool) *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

func loadConfig(path string) (config.Config, error) {
	cfg, err := config.Load(path)
	if err != nil {
		return config.Default(), err
	}
	return cfg, nil
}

func saveConfig(path string, cfg config.Config) error {
	return config.Save(path, cfg)
}

func printJSON(out io.Writer, value any) error {
	enc := jsonEncoder{w: out}
	return enc.Encode(value)
}

type jsonEncoder struct {
	w io.Writer
}

func (e jsonEncoder) Encode(value any) error {
	data, err := marshalPretty(value)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(e.w, string(data))
	return err
}
