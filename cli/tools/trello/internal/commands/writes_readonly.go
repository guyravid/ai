//go:build readonly

package commands

import "github.com/guyravid/ai/cli/tools/trello/internal/registry"

// WritesEnabled reports whether this build includes write commands.
const WritesEnabled = false

// Writes returns nothing in a read-only build: the write commands are not compiled in.
func Writes() []*registry.Command { return nil }
