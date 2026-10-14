package config

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Nested blocks (contract §13.4 rule 7, optional since 1.4.3). A tool opts in by giving a setting a
// Def.FilePath, or a credential a path in Input.CredentialPaths. A path is file syntax only: the
// setting keeps its flat name for the environment, flags, and list-config, and the file loader
// flattens every section to flat keys so resolution never sees the nesting. A tool that declares no
// path keeps the flat file format of earlier versions, and rejects blocks as unrecognised members.

type memberKind int

const (
	memberSetting memberKind = iota
	memberCredentialValue
	memberCredentialFile
	memberEnvFile
	memberDescription
)

// layoutNode is one member of a config file section: a block when children is non-nil, otherwise a
// leaf that stands for the flat key flatKey.
type layoutNode struct {
	children map[string]*layoutNode
	kind     memberKind
	flatKey  string
	def      *Def
}

// Layout is the shape of a config file section, built from the settings and credentials a tool
// declares. It is the one place that knows where each flat key lives in the file.
type Layout struct {
	root  *layoutNode
	paths map[string][]string // flat key -> member path within a section
}

var segmentPattern = regexp.MustCompile(`^[a-z0-9]+(_[a-z0-9]+)*$`)

// NewLayout builds and checks a section layout. credentialPaths gives the nested path of a
// credential's value member; its file member is the same path with "_file" on the last segment.
// An error here is a defect in the tool's declarations, not in a config file: two paths that
// flatten to one name (rule 7.2), a path that does not flatten to its setting's name (rule 7.1), a
// block that is also a member, or a nested setting whose flag is not its flat name.
func NewLayout(defs []*Def, credentials []string, credentialPaths map[string][]string) (*Layout, error) {
	layout := &Layout{root: &layoutNode{children: map[string]*layoutNode{}}, paths: map[string][]string{}}
	for _, def := range defs {
		if !def.InFile {
			continue
		}
		path := def.FilePath
		if path == nil {
			path = []string{def.FileKey()}
		} else if err := checkNestedDef(def); err != nil {
			return nil, err
		}
		if err := layout.add(path, def.Name, &layoutNode{kind: memberSetting, def: def}); err != nil {
			return nil, err
		}
	}
	for _, name := range credentials {
		valuePath := credentialPaths[name]
		if valuePath == nil {
			valuePath = []string{strings.ToLower(name)}
		}
		filePath := append(append([]string{}, valuePath[:len(valuePath)-1]...), valuePath[len(valuePath)-1]+"_file")
		if err := layout.add(valuePath, name, &layoutNode{kind: memberCredentialValue}); err != nil {
			return nil, err
		}
		if err := layout.add(filePath, name+"_FILE", &layoutNode{kind: memberCredentialFile}); err != nil {
			return nil, err
		}
	}
	if err := layout.add([]string{"env_file"}, "ENV_FILE", &layoutNode{kind: memberEnvFile}); err != nil {
		return nil, err
	}
	if err := layout.add([]string{DescriptionKey}, strings.ToUpper(DescriptionKey), &layoutNode{kind: memberDescription}); err != nil {
		return nil, err
	}
	return layout, nil
}

func checkNestedDef(def *Def) error {
	if def.Flag != "" && def.Flag != "--"+strings.ReplaceAll(strings.ToLower(def.Name), "_", "-") {
		return fmt.Errorf("setting %s is nested, so its flag must be --%s, not %s",
			def.Name, strings.ReplaceAll(strings.ToLower(def.Name), "_", "-"), def.Flag)
	}
	return nil
}

// add places one leaf at path, which must flatten to name.
func (l *Layout) add(path []string, name string, leaf *layoutNode) error {
	dotted := strings.Join(path, ".")
	if len(path) == 0 {
		return fmt.Errorf("%s has an empty path", name)
	}
	for _, segment := range path {
		if !segmentPattern.MatchString(segment) {
			return fmt.Errorf("path %s of %s: segment %q must be lowercase letters and digits, joined by single underscores", dotted, name, segment)
		}
	}
	if flat := strings.ToUpper(strings.Join(path, "_")); flat != name {
		return fmt.Errorf("path %s flattens to %s, not to its name %s", dotted, flat, name)
	}
	flatKey := strings.ToLower(name)
	if existing, ok := l.paths[flatKey]; ok {
		return fmt.Errorf("paths %s and %s both flatten to %s", strings.Join(existing, "."), dotted, name)
	}
	node := l.root
	for index, segment := range path[:len(path)-1] {
		child, ok := node.children[segment]
		if !ok {
			child = &layoutNode{children: map[string]*layoutNode{}}
			node.children[segment] = child
		} else if child.children == nil {
			return fmt.Errorf("%s is a member, so it cannot also be the block holding %s",
				strings.Join(path[:index+1], "."), dotted)
		}
		node = child
	}
	last := path[len(path)-1]
	if existing, ok := node.children[last]; ok {
		if existing.children != nil {
			return fmt.Errorf("%s is a block, so it cannot also be a member", dotted)
		}
		return fmt.Errorf("%s is declared twice", dotted)
	}
	leaf.flatKey = flatKey
	node.children[last] = leaf
	l.paths[flatKey] = append([]string{}, path...)
	return nil
}

// Path returns the member path of a flat key within a section, such as "telemetry.otlp.endpoint"
// for "telemetry_otlp_endpoint". An undeclared key is its own path.
func (l *Layout) Path(flatKey string) string {
	if l != nil {
		if path, ok := l.paths[flatKey]; ok {
			return strings.Join(path, ".")
		}
	}
	return flatKey
}

// Nested reports whether any member is declared inside a block.
func (l *Layout) Nested() bool {
	for _, path := range l.paths {
		if len(path) > 1 {
			return true
		}
	}
	return false
}

// spellingOf returns the declared path of a member written at an undeclared path whose flat name is
// known, such as "telemetry.otlp.endpoint" for "telemetry_otlp_endpoint" (rule 7.3), or "".
func (l *Layout) spellingOf(path []string) string {
	declared, ok := l.paths[strings.Join(path, "_")]
	if !ok || strings.Join(declared, ".") == strings.Join(path, ".") {
		return ""
	}
	return strings.Join(declared, ".")
}

// NestSchema arranges config-file JSON Schema properties, keyed by flat key, into the layout's blocks.
// Each block becomes an object schema that admits only its declared members. A key the layout does
// not declare stays at the top level. With no nested member, the result equals the input.
func (l *Layout) NestSchema(flat map[string]any) map[string]any {
	properties := map[string]any{}
	keys := make([]string, 0, len(flat))
	for key := range flat {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		path, ok := l.paths[key]
		if !ok {
			path = []string{key}
		}
		target := properties
		for _, segment := range path[:len(path)-1] {
			block, ok := target[segment].(map[string]any)
			if !ok {
				block = map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{}}
				target[segment] = block
			}
			target = block["properties"].(map[string]any)
		}
		target[path[len(path)-1]] = flat[key]
	}
	return properties
}
