// Command gendocs writes mandoc(1) man pages for the mnemosyne CLI to a
// target directory (default ./man/man1) using cobra/doc.GenManTree.
package main

import (
	"log"
	"os"

	"github.com/spf13/cobra/doc"

	"github.com/AletheiaResearch/mnemosyne/internal/cli"
)

func main() {
	out := "man/man1"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		log.Fatalf("mkdir %s: %v", out, err)
	}
	header := &doc.GenManHeader{
		Title:   "MNEMOSYNE",
		Section: "1",
		Source:  "Mnemosyne",
		Manual:  "Mnemosyne Manual",
	}
	if err := doc.GenManTree(cli.NewDocsRoot(), header, out); err != nil {
		log.Fatalf("gen man tree: %v", err)
	}
}
