package config

import (
	"bytes"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

// mergeChangesIntoFile writes the changes a save makes into the existing
// file's own YAML, instead of replacing the file with the marshalled struct.
//
// The marshalled struct carries every default Load filled in and none of the
// user's comments, so saving one preference turned a five-line config into
// forty-five. The changes are what differs between next (the config being
// saved) and the config the file already stands for — the file loaded and
// rendered the same way — so a default nobody touched is not a change.
//
// ok is false when there is nothing to merge into (the file does not parse,
// does not load, or is not a mapping); the caller then writes next whole.
func mergeChangesIntoFile(path string, existing, next []byte) ([]byte, bool) {
	var doc yaml.Node
	if err := yaml.Unmarshal(existing, &doc); err != nil || doc.Kind != yaml.DocumentNode ||
		len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, false
	}
	baseCfg, err := Load(path)
	if err != nil {
		return nil, false
	}
	baseData, err := renderForSave(path, baseCfg)
	if err != nil {
		return nil, false
	}
	var base, want map[string]any
	if yaml.Unmarshal(baseData, &base) != nil || yaml.Unmarshal(next, &want) != nil {
		return nil, false
	}

	if !applyMapChanges(doc.Content[0], base, want) {
		return existing, true
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(indentOf(existing))
	if err := enc.Encode(&doc); err != nil {
		return nil, false
	}
	if err := enc.Close(); err != nil {
		return nil, false
	}
	return buf.Bytes(), true
}

// applyMapChanges edits node so that the keys differing between base and want
// take want's values: changed and added keys are set, keys want dropped are
// removed. A nested mapping is edited key by key, so a change deep inside does
// not write its siblings. Reports whether node changed.
func applyMapChanges(node *yaml.Node, base, want map[string]any) bool {
	changed := false
	for k, wv := range want {
		bv, inBase := base[k]
		if inBase && reflect.DeepEqual(bv, wv) {
			continue
		}
		wm, wantMap := wv.(map[string]any)
		if wantMap {
			bm, _ := bv.(map[string]any)
			child := mappingValue(node, k)
			if child == nil || child.Kind != yaml.MappingNode {
				child = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
				// A mapping the file lacks gets only the changed keys; one
				// holding a scalar is replaced.
				if !applyMapChanges(child, bm, wm) {
					continue
				}
				setMappingValue(node, k, child)
				changed = true
				continue
			}
			if applyMapChanges(child, bm, wm) {
				changed = true
			}
			continue
		}
		var v yaml.Node
		if err := v.Encode(wv); err != nil {
			continue
		}
		if cur := mappingValue(node, k); cur != nil && sameScalar(cur, &v) {
			continue
		}
		setMappingValue(node, k, &v)
		changed = true
	}
	for k := range base {
		if _, kept := want[k]; kept {
			continue
		}
		if removeMappingKey(node, k) {
			changed = true
		}
	}
	return changed
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// setMappingValue replaces key's value in place, keeping the key node and its
// comments, or appends the key when the mapping lacks it.
func setMappingValue(node *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			old := node.Content[i+1]
			value.LineComment = old.LineComment
			if value.Kind == yaml.ScalarNode && old.Kind == yaml.ScalarNode && value.Tag == old.Tag {
				value.Style = old.Style
			}
			node.Content[i+1] = value
			return
		}
	}
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		value)
}

func removeMappingKey(node *yaml.Node, key string) bool {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			node.Content = append(node.Content[:i], node.Content[i+2:]...)
			return true
		}
	}
	return false
}

func sameScalar(a, b *yaml.Node) bool {
	return a.Kind == yaml.ScalarNode && b.Kind == yaml.ScalarNode && a.Tag == b.Tag && a.Value == b.Value
}

// indentOf is the indentation the file already uses for nested keys, so the
// re-encoded file keeps its look; 2 when it has no nesting to go by.
func indentOf(data []byte) int {
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimLeft(line, " ")
		if n := len(line) - len(trimmed); n > 0 && trimmed != "" && !strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "- ") {
			return n
		}
	}
	return 2
}
