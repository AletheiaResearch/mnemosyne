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
	var input string
	var output string
	var format string

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
			serializer := serialize.Lookup(format)
			if serializer == nil {
				return errors.New("unknown serializer")
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
	return cmd
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
