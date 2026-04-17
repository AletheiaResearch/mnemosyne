# Serializers

- `canonical`: Mnemosyne's native record shape.
- `anthropic`: message-array export with role/content pairs.
- `openai`: chat-completions style messages.
- `chatml`: tagged transcript text.
- `zephyr`: turn-delimited plain-text transcript.
- `flat`: compact transcript per record.

All serializers read canonical JSONL and preserve record ordering.
