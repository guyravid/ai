package registry

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/guyravid/ai/cli/tools/trello/internal/errs"
)

var segmentPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

var reservedGroups = map[string]bool{
	"tools": true, "describe": true, "teach": true, "doctor": true, "list-config": true,
	"dataset": true, "serve": true, "version": true, "write": true, "help": true,
}

// Validate enforces the registry rules of base template §2 at startup, before any command runs.
func (r *Registry) Validate() error {
	seen := map[string]bool{}
	for _, command := range r.commands {
		if seen[command.Name] {
			return fmt.Errorf("command %s is defined twice", command.Name)
		}
		seen[command.Name] = true
		if err := validateCommand(command); err != nil {
			return fmt.Errorf("command %s: %w", command.Name, err)
		}
	}
	return nil
}

func validateCommand(command *Command) error {
	segments := command.Argv()
	for _, segment := range segments {
		if !segmentPattern.MatchString(segment) {
			return fmt.Errorf("segment %q must match [a-z][a-z0-9-]*", segment)
		}
	}
	if command.Kind != KindBuiltin {
		group := segments[0]
		if command.HasWritePrefix() {
			if len(segments) < 3 {
				return fmt.Errorf("a write command needs a group and a verb after write")
			}
			group = segments[1]
		} else if len(segments) < 2 {
			return fmt.Errorf("a domain command needs at least two segments")
		}
		if reservedGroups[group] {
			return fmt.Errorf("group %q is reserved", group)
		}
		if command.Description == "" || len(command.Description) > 160 || strings.Contains(command.Description, "\n") {
			return fmt.Errorf("description must be one line of at most 160 characters")
		}
	}
	if command.Annotations.ReadOnlyHint == command.IsWrite() {
		return fmt.Errorf("readOnlyHint must be true exactly when the command is not mutating")
	}
	if command.HasWritePrefix() && !command.IsWrite() {
		return fmt.Errorf("only a mutating command may use the write prefix")
	}
	if command.Collectable && !command.Paged {
		return fmt.Errorf("collectable requires paged")
	}
	if command.TimeWindow && !command.Paged {
		return fmt.Errorf("a time window applies only to list commands")
	}
	if command.Kind == KindList && (command.Fields == nil || command.Sort == "" || command.Limits == nil) {
		return fmt.Errorf("a list command declares fields, sort, and limits")
	}
	for _, param := range command.Params {
		name := strings.TrimPrefix(param.Flag, "--")
		if param.Positional {
			continue
		}
		if ReservedFlag(name) != nil {
			return fmt.Errorf("parameter flag %s collides with a reserved flag", param.Flag)
		}
		for _, forbidden := range ForbiddenFlags {
			if name == forbidden {
				return fmt.Errorf("parameter flag %s is a forbidden credential flag", param.Flag)
			}
		}
	}
	valid := map[errs.Code]bool{}
	for _, code := range errs.Codes() {
		valid[code] = true
	}
	for _, code := range command.Errors {
		if !valid[code] {
			return fmt.Errorf("error code %q is not in the contract", code)
		}
	}
	return nil
}

// ValidateMutating checks that the names-only list a read-only build refuses matches the mutating
// commands of a full build, so the two cannot drift apart.
func (r *Registry) ValidateMutating(names []string) error {
	declared := map[string]bool{}
	for _, name := range names {
		declared[name] = true
	}
	count := 0
	for _, command := range r.commands {
		if !command.IsWrite() {
			continue
		}
		count++
		if !declared[command.Name] {
			return fmt.Errorf("mutating command %s is missing from the mutating names list", command.Name)
		}
	}
	if count > 0 && count != len(names) {
		return fmt.Errorf("the mutating names list has %d entries but the registry has %d mutating commands", len(names), count)
	}
	return nil
}
