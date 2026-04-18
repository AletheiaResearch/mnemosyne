package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/AletheiaResearch/mnemosyne/internal/config"
	"github.com/AletheiaResearch/mnemosyne/internal/schema"
	"github.com/AletheiaResearch/mnemosyne/internal/serialize"
	"github.com/AletheiaResearch/mnemosyne/internal/serialize/toolcatalog"
)

func newTransformCommand(rt *runtime) *cobra.Command {
	var (
		input               string
		output              string
		format              string
		templateName        string
		templateFile        string
		bosToken            string
		eosToken            string
		addGenerationPrompt bool
		toolsFile           string
		noTools             bool
	)

	cmd := &cobra.Command{
		Use:   "transform",
		Short: "Transform canonical JSONL into another serializer format",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if input == "" {
				return errors.New("transform requires --input")
			}
			if output == "" {
				return errors.New("transform requires --output")
			}

			cfg, err := loadConfig(rt.configPath)
			if err != nil {
				return err
			}

			flags := transformFlags{
				Format:              format,
				TemplateName:        templateName,
				TemplateFile:        templateFile,
				BOSToken:            bosToken,
				EOSToken:            eosToken,
				AddGenerationPrompt: addGenerationPrompt,
			}

			usingTemplate, err := templatePathSelected(cmd, cfg.ChatTemplateValue(), flags)
			if err != nil {
				return err
			}

			var tools []serialize.ToolSchema
			if usingTemplate && !noTools {
				tools, err = resolveTransformTools(input, toolsFile)
				if err != nil {
					return err
				}
			}

			serializer, err := resolveTransformSerializer(cmd, cfg.ChatTemplateValue(), flags, tools)
			if err != nil {
				return err
			}

			in, err := os.Open(input)
			if err != nil {
				return err
			}
			defer in.Close()

			if dir := filepath.Dir(output); dir != "" && dir != "." {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return err
				}
			}

			out, err := os.Create(output)
			if err != nil {
				return err
			}
			defer out.Close()

			return transformRecords(in, out, serializer)
		},
	}

	cmd.Flags().StringVar(&input, "input", "", "path to canonical JSONL")
	cmd.Flags().StringVar(&output, "output", "", "path to transformed output")
	cmd.Flags().StringVar(&format, "format", "canonical", "serializer to use")
	cmd.Flags().StringVar(&templateName, "template-name", "", "render through a builtin chat template (e.g. chatml, hermes, deepseekr1)")
	cmd.Flags().StringVar(&templateFile, "template-file", "", "render through a chat template loaded from this file (.tmpl or .jinja)")
	cmd.Flags().StringVar(&bosToken, "bos-token", "", "value exposed to templates as bos_token")
	cmd.Flags().StringVar(&eosToken, "eos-token", "", "value exposed to templates as eos_token")
	cmd.Flags().BoolVar(&addGenerationPrompt, "add-generation-prompt", false, "set add_generation_prompt to true for template rendering")
	cmd.Flags().StringVar(&toolsFile, "tools-file", "", "JSON file of tool schemas to expose as `tools` in template context (overrides inference)")
	cmd.Flags().BoolVar(&noTools, "no-tools", false, "skip tool inference and leave the template's tools list empty")
	return cmd
}

type transformFlags struct {
	Format              string
	TemplateName        string
	TemplateFile        string
	BOSToken            string
	EOSToken            string
	AddGenerationPrompt bool
}

// templatePathSelected reports whether the effective serializer is a template
// (either explicit flag or persisted default).
func templatePathSelected(cmd *cobra.Command, defaults config.ChatTemplate, f transformFlags) (bool, error) {
	flagChanged := cmd.Flags().Changed
	templateNameSet := flagChanged("template-name")
	templateFileSet := flagChanged("template-file")
	if templateNameSet && templateFileSet {
		return false, errors.New("--template-name and --template-file are mutually exclusive")
	}
	formatSet := flagChanged("format")
	switch {
	case templateNameSet && f.TemplateName != "":
		return true, nil
	case templateFileSet && f.TemplateFile != "":
		return true, nil
	case formatSet:
		return false, nil
	case defaults.Name != "" || defaults.File != "":
		return true, nil
	}
	return false, nil
}

// resolveTransformTools runs the infer → catalog → override chain. Origin(s)
// are discovered while scanning records, so a dataset mixing harnesses pulls
// in all relevant catalogs. --tools-file replaces the result when supplied.
func resolveTransformTools(inputPath, toolsFile string) ([]serialize.ToolSchema, error) {
	if toolsFile != "" {
		f, err := os.Open(toolsFile)
		if err != nil {
			return nil, fmt.Errorf("open tools file: %w", err)
		}
		defer f.Close()
		return serialize.ReadToolSchemas(f)
	}

	inferred, origins, err := inferToolsAndOrigins(inputPath)
	if err != nil {
		return nil, err
	}
	catalog := catalogFromOrigins(origins)
	return serialize.MergeToolCatalog(inferred, catalog), nil
}

func inferToolsAndOrigins(path string) ([]serialize.ToolSchema, []string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	// Capture origins in a single pass by copying through a tee.
	var originBuf []string
	seen := map[string]struct{}{}
	pr, pw := io.Pipe()
	errCh := make(chan error, 1)
	go func() {
		defer pw.Close()
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), schema.MaxJSONLineBytes)
		for scanner.Scan() {
			line := scanner.Bytes()
			if _, err := pw.Write(append(append([]byte{}, line...), '\n')); err != nil {
				errCh <- err
				return
			}
			var head struct {
				Origin string `json:"origin"`
			}
			if err := json.Unmarshal(line, &head); err == nil && head.Origin != "" {
				if _, dup := seen[head.Origin]; !dup {
					seen[head.Origin] = struct{}{}
					originBuf = append(originBuf, head.Origin)
				}
			}
		}
		errCh <- scanner.Err()
	}()

	tools, inferErr := serialize.InferTools(pr)
	if inferErr != nil {
		return nil, nil, inferErr
	}
	if err := <-errCh; err != nil {
		return nil, nil, err
	}
	return tools, originBuf, nil
}

func catalogFromOrigins(origins []string) []serialize.ToolSchema {
	var merged []serialize.ToolSchema
	for _, origin := range origins {
		reader := toolcatalog.LoadAsReader(origin)
		if reader == nil {
			continue
		}
		tools, err := serialize.ReadToolSchemas(reader)
		if err != nil {
			continue
		}
		merged = append(merged, tools...)
	}
	return merged
}

// resolveTransformSerializer picks the right serializer, applying persisted
// chat-template defaults when the caller did not set the relevant CLI flag.
//
// Precedence for the serializer identity:
//  1. Explicit --template-name / --template-file flag.
//  2. Explicit --format flag.
//  3. Persisted ChatTemplate in config (Name preferred, then File).
//  4. Default --format value ("canonical").
//
// Token and generation-prompt flags fall back to persisted values the same way.
func resolveTransformSerializer(cmd *cobra.Command, defaults config.ChatTemplate, f transformFlags, tools []serialize.ToolSchema) (serialize.Serializer, error) {
	flagChanged := cmd.Flags().Changed
	templateNameSet := flagChanged("template-name")
	templateFileSet := flagChanged("template-file")
	formatSet := flagChanged("format")

	if templateNameSet && templateFileSet {
		return nil, errors.New("--template-name and --template-file are mutually exclusive")
	}

	opts := serialize.TemplateOptions{
		BOSToken:            f.BOSToken,
		EOSToken:            f.EOSToken,
		AddGenerationPrompt: f.AddGenerationPrompt,
		Tools:               tools,
	}
	if !flagChanged("bos-token") {
		opts.BOSToken = defaults.BOSToken
	}
	if !flagChanged("eos-token") {
		opts.EOSToken = defaults.EOSToken
	}
	if !flagChanged("add-generation-prompt") {
		opts.AddGenerationPrompt = defaults.AddGenerationPrompt
	}

	switch {
	case templateNameSet && f.TemplateName != "":
		return serialize.NewBuiltinTemplate(f.TemplateName, opts)
	case templateFileSet && f.TemplateFile != "":
		return serialize.NewFileTemplate(f.TemplateFile, opts)
	case formatSet:
		// Explicit --format wins over persisted template defaults.
	case defaults.Name != "":
		return serialize.NewBuiltinTemplate(defaults.Name, opts)
	case defaults.File != "":
		return serialize.NewFileTemplate(defaults.File, opts)
	}

	serializer := serialize.Lookup(f.Format)
	if serializer == nil {
		return nil, errors.New("unknown serializer")
	}
	return serializer, nil
}

func transformRecords(in io.Reader, out io.Writer, serializer serialize.Serializer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), schema.MaxJSONLineBytes)
	writer := bufio.NewWriter(out)

	for scanner.Scan() {
		var record schema.Record
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return err
		}
		payload, err := serializer.Serialize(record)
		if err != nil {
			return err
		}
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		if _, err := writer.Write(append(data, '\n')); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return writer.Flush()
}
