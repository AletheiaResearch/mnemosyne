package serialize

func Registry() []Serializer {
	return []Serializer{
		Canonical{},
		Anthropic{},
		OpenAI{},
		ChatML{},
		Zephyr{},
		Flat{},
		OpenTraces{},
	}
}

func Lookup(name string) Serializer {
	for _, serializer := range Registry() {
		if serializer.Name() == name {
			return serializer
		}
	}
	return nil
}
