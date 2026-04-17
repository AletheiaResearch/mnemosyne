package tui

func extractScreen() screenDescriptor {
	return screenDescriptor{
		Title: "Extract",
		Body:  "Extract writes canonical JSONL to a local file, applies anonymization and redaction, updates run state, and stops before any network I/O.\n\nTypical command:\n  mnemosyne extract --scope all",
	}
}

func transformScreen() screenDescriptor {
	return screenDescriptor{
		Title: "Transform",
		Body:  "Transform rewrites canonical JSONL into one of the supported serializer outputs:\n  canonical, anthropic, openai, chatml, zephyr, flat",
	}
}

func validateScreen() screenDescriptor {
	return screenDescriptor{
		Title: "Validate",
		Body:  "Validate checks canonical JSONL shape and per-record invariants before publication or downstream analysis.",
	}
}
