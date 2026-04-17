package tui

func surveyScreen() screenDescriptor {
	return screenDescriptor{
		Title: "Survey",
		Body:  "Survey recomputes workflow state, checks Hugging Face identity if available, and lists detected source groupings.\n\nUse the CLI when you need structured output:\n  mnemosyne survey\n  mnemosyne inspect --scope all",
	}
}

func configureScreen() screenDescriptor {
	return screenDescriptor{
		Title: "Configure",
		Body:  "Configure sets the persistent scope, exclusions, literal redactions, and handle anonymization.\n\nTypical command:\n  mnemosyne configure --scope all --confirm-scope",
	}
}
