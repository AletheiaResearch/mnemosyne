package tui

func cardScreen() screenDescriptor {
	return screenDescriptor{
		Title: "Dataset Card",
		Body:  "Publish generates a manifest and README.md dataset card from the attested export summary before uploading to the dataset repo.",
	}
}
