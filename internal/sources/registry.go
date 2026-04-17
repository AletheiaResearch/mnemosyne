package sources

import (
	"github.com/AletheiaResearch/mnemosyne/internal/source"
	"github.com/AletheiaResearch/mnemosyne/internal/source/claudecode"
	"github.com/AletheiaResearch/mnemosyne/internal/source/codex"
	"github.com/AletheiaResearch/mnemosyne/internal/source/cursor"
	"github.com/AletheiaResearch/mnemosyne/internal/source/gemini"
	"github.com/AletheiaResearch/mnemosyne/internal/source/kimi"
	"github.com/AletheiaResearch/mnemosyne/internal/source/openclaw"
	"github.com/AletheiaResearch/mnemosyne/internal/source/opencode"
	"github.com/AletheiaResearch/mnemosyne/internal/source/orchestrator"
	"github.com/AletheiaResearch/mnemosyne/internal/source/supplied"
)

func Registry() []source.Source {
	return []source.Source{
		claudecode.New(""),
		codex.New("", ""),
		cursor.New(""),
		gemini.New(""),
		kimi.New(""),
		openclaw.New(""),
		opencode.New(""),
		orchestrator.New(""),
		supplied.New(""),
	}
}
