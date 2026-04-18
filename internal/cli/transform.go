package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/AletheiaResearch/mnemosyne/internal/schema"
	"github.com/AletheiaResearch/mnemosyne/internal/serialize"
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

			serializer, err := selectSerializer(format, templateName, templateFile, serialize.TemplateOptions{
				BOSToken:            bosToken,
				EOSToken:            eosToken,
				AddGenerationPrompt: addGenerationPrompt,
			})
			if err != nil {
				return err
			}

			in, err := os.Open(input)
			if err != nil {
				return err
			}
			defer in.Close()

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
	cmd.Flags().StringVar(&templateName, "template-name", "", "render through a builtin chat template (e.g. chatml, zephyr, vicuna)")
	cmd.Flags().StringVar(&templateFile, "template-file", "", "render through a chat template loaded from this file")
	cmd.Flags().StringVar(&bosToken, "bos-token", "", "value exposed to templates as .BOSToken")
	cmd.Flags().StringVar(&eosToken, "eos-token", "", "value exposed to templates as .EOSToken")
	cmd.Flags().BoolVar(&addGenerationPrompt, "add-generation-prompt", false, "set .AddGenerationPrompt to true for template rendering")
	return cmd
}

func selectSerializer(format, templateName, templateFile string, opts serialize.TemplateOptions) (serialize.Serializer, error) {
	if templateName != "" && templateFile != "" {
		return nil, errors.New("--template-name and --template-file are mutually exclusive")
	}
	if templateName != "" {
		return serialize.NewBuiltinTemplate(templateName, opts)
	}
	if templateFile != "" {
		return serialize.NewFileTemplate(templateFile, opts)
	}
	serializer := serialize.Lookup(format)
	if serializer == nil {
		return nil, errors.New("unknown serializer")
	}
	return serializer, nil
}

func transformRecords(in io.Reader, out io.Writer, serializer serialize.Serializer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
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
