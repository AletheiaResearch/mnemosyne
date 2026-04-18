package cli

import (
	"github.com/spf13/cobra"

	"github.com/AletheiaResearch/mnemosyne/internal/config"
)

func newConfigureCommand(rt *runtime) *cobra.Command {
	var repo string
	var scope string
	var excludes []string
	var redactions []string
	var handles []string
	var confirmScope bool

	var chatTemplateName string
	var chatTemplateFile string
	var chatTemplateBOSToken string
	var chatTemplateEOSToken string
	var chatTemplateAddGenerationPrompt bool
	var chatTemplateClear bool

	cmd := &cobra.Command{
		Use:   "configure",
		Short: "Read or modify persistent settings",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(rt.configPath)
			if err != nil {
				return err
			}

			chatTemplateTouched := cmd.Flags().Changed("chat-template-name") ||
				cmd.Flags().Changed("chat-template-file") ||
				cmd.Flags().Changed("chat-template-bos-token") ||
				cmd.Flags().Changed("chat-template-eos-token") ||
				cmd.Flags().Changed("chat-template-add-generation-prompt") ||
				chatTemplateClear

			changed := repo != "" || scope != "" || len(excludes) > 0 ||
				len(redactions) > 0 || len(handles) > 0 || confirmScope ||
				chatTemplateTouched
			if !changed {
				return printJSON(cmd.OutOrStdout(), cfg.Masked())
			}

			if repo != "" {
				cfg.DestinationRepo = repo
			}
			if scope != "" {
				cfg.OriginScope = scope
			}
			cfg.MergeStringSlice(&cfg.ExcludedGroupings, excludes)
			cfg.MergeStringSlice(&cfg.CustomRedactions, redactions)
			cfg.MergeStringSlice(&cfg.CustomHandles, handles)
			cfg.ScopeConfirmed = cfg.ScopeConfirmed || confirmScope || len(excludes) > 0

			if chatTemplateTouched {
				tmpl := cfg.ChatTemplateValue()
				if chatTemplateClear {
					tmpl = config.ChatTemplate{}
				}
				if cmd.Flags().Changed("chat-template-name") {
					tmpl.Name = chatTemplateName
				}
				if cmd.Flags().Changed("chat-template-file") {
					tmpl.File = chatTemplateFile
				}
				if cmd.Flags().Changed("chat-template-bos-token") {
					tmpl.BOSToken = chatTemplateBOSToken
				}
				if cmd.Flags().Changed("chat-template-eos-token") {
					tmpl.EOSToken = chatTemplateEOSToken
				}
				if cmd.Flags().Changed("chat-template-add-generation-prompt") {
					tmpl.AddGenerationPrompt = chatTemplateAddGenerationPrompt
				}
				if tmpl.IsEmpty() {
					cfg.ChatTemplate = nil
				} else {
					cfg.ChatTemplate = &tmpl
				}
			}

			cfg.RefreshPhase(false)

			if err := saveConfig(rt.configPath, cfg); err != nil {
				return err
			}
			return printJSON(cmd.OutOrStdout(), cfg.Masked())
		},
	}

	cmd.Flags().StringVar(&repo, "destination-repo", "", "dataset repository identifier")
	cmd.Flags().StringVar(&scope, "scope", "", "origin scope to extract")
	cmd.Flags().StringSliceVar(&excludes, "exclude", nil, "grouping label to exclude")
	cmd.Flags().StringSliceVar(&redactions, "redact", nil, "literal string to redact")
	cmd.Flags().StringSliceVar(&handles, "handle", nil, "handle to anonymize")
	cmd.Flags().BoolVar(&confirmScope, "confirm-scope", false, "mark grouping selection as confirmed")
	cmd.Flags().StringVar(&chatTemplateName, "chat-template-name", "", "default builtin chat template for transform (e.g. chatml, zephyr, vicuna)")
	cmd.Flags().StringVar(&chatTemplateFile, "chat-template-file", "", "default path to a custom chat template for transform")
	cmd.Flags().StringVar(&chatTemplateBOSToken, "chat-template-bos-token", "", "default BOS token supplied to chat templates")
	cmd.Flags().StringVar(&chatTemplateEOSToken, "chat-template-eos-token", "", "default EOS token supplied to chat templates")
	cmd.Flags().BoolVar(&chatTemplateAddGenerationPrompt, "chat-template-add-generation-prompt", false, "default value of the add_generation_prompt flag for chat templates")
	cmd.Flags().BoolVar(&chatTemplateClear, "chat-template-clear", false, "clear persisted chat template defaults")
	cmd.ValidArgsFunction = func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return []string{
			string(config.PhaseInitial),
			string(config.PhasePreparing),
			string(config.PhasePendingReview),
			string(config.PhaseCleared),
			string(config.PhaseFinalized),
		}, cobra.ShellCompDirectiveNoFileComp
	}
	return cmd
}
