package tui

func publishScreen() screenDescriptor {
	return screenDescriptor{
		Title: "Publish",
		Body:  "Publish revalidates the attestation gate, checks Hugging Face credentials, creates or reuses a dataset repo, and uploads the export, manifest, and README card.\n\nTypical command:\n  mnemosyne publish --publish-attestation \"...\"",
	}
}
