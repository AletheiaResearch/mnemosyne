package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Quantumlyy/mnemosyne/internal/schema"
)

func newValidateCommand(rt *runtime) *cobra.Command {
	var input string
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate a canonical JSONL export",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if input == "" {
				return errors.New("validate requires --input")
			}
			file, err := os.Open(input)
			if err != nil {
				return err
			}
			defer file.Close()

			scanner := bufio.NewScanner(file)
			scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
			line := 0
			valid := 0
			for scanner.Scan() {
				line++
				if scanner.Text() == "" {
					return fmt.Errorf("line %d: empty lines are not allowed", line)
				}
				var record schema.Record
				if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
					return fmt.Errorf("line %d: %w", line, err)
				}
				if err := schema.ValidateRecord(record); err != nil {
					return fmt.Errorf("line %d: %w", line, err)
				}
				valid++
			}
			if err := scanner.Err(); err != nil {
				return err
			}
			return printJSON(cmd.OutOrStdout(), map[string]any{
				"input":         input,
				"valid_records": valid,
			})
		},
	}
	cmd.Flags().StringVar(&input, "input", "", "path to the JSONL file to validate")
	return cmd
}
