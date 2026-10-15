// Package operations parses the Runner's explicit, finite capability set.
package operations

import (
	"errors"
	"strings"

	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

var ErrInvalid = errors.New("invalid runner operations")

// Selection is immutable. Its zero value enables no operation.
type Selection struct{ bits uint8 }

// Parse accepts comma-separated operation names without duplicates or whitespace.
// An empty value preserves the default with no operations enabled.
func Parse(value string) (Selection, error) {
	var selected Selection
	if value == "" {
		return selected, nil
	}
	for _, name := range strings.Split(value, ",") {
		bit := operationBit(name)
		if bit == 0 || selected.bits&bit != 0 {
			return Selection{}, ErrInvalid
		}
		selected.bits |= bit
	}
	return selected, nil
}

func (s Selection) Contains(name string) bool {
	bit := operationBit(name)
	return bit != 0 && s.bits&bit != 0
}

// Names returns a new slice in a fixed order, independent of configuration order.
func (s Selection) Names() []string {
	names := []string{}
	for _, name := range []string{p.ReadFileOperation, p.ListDirectoryOperation, p.FindFilesOperation, p.WriteFileOperation, p.RunCommandOperation, p.SkillPrepareOperation} {
		if s.Contains(name) {
			names = append(names, name)
		}
	}
	return names
}

func operationBit(name string) uint8 {
	switch name {
	case p.ReadFileOperation:
		return 1
	case p.ListDirectoryOperation:
		return 2
	case p.FindFilesOperation:
		return 4
	case p.WriteFileOperation:
		return 8
	case p.RunCommandOperation:
		return 16
	case p.SkillPrepareOperation:
		return 32
	default:
		return 0
	}
}
