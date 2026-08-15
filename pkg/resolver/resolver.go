package resolver

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

var reservedKeys = map[string]bool{
	"stages": true, "default": true, "variables": true,
	"include": true, "workflow": true, "image": true,
	"before_script": true, "after_script": true, "services": true,
	"cache": true,
}

func Resolve(data []byte, baseDir string) ([]byte, error) {
	seen := map[string]bool{}
	return resolve(data, baseDir, seen)
}

func resolve(data []byte, baseDir string, seen map[string]bool) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("unmarshal YAML: %w", err)
	}
	if len(doc.Content) == 0 {
		return data, nil
	}

	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return data, nil
	}

	if err := resolveIncludes(root, baseDir, seen); err != nil {
		return nil, fmt.Errorf("resolving includes: %w", err)
	}
	resolveExtends(root)
	root = expandReferences(root, root)
	doc.Content[0] = root

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	if err := enc.Encode(&doc); err != nil {
		return nil, fmt.Errorf("marshal resolved YAML: %w", err)
	}
	enc.Close()
	return buf.Bytes(), nil
}

func resolveIncludes(root *yaml.Node, baseDir string, seen map[string]bool) error {
	idx := findMappingIndex(root, "include")
	if idx < 0 {
		return nil
	}

	includeNode := root.Content[idx+1]
	includes, err := normalizeIncludes(includeNode)
	if err != nil {
		return err
	}

	for _, inc := range includes {
		if inc.local == "" {
			continue
		}
		if !filepath.IsAbs(inc.local) {
			if baseDir == "" {
				baseDir = "."
			}
			inc.local = filepath.Join(baseDir, inc.local)
		}
		abs, err := filepath.Abs(inc.local)
		if err != nil {
			return fmt.Errorf("include %q: %w", inc.local, err)
		}
		if seen[abs] {
			return fmt.Errorf("cyclic include: %s", abs)
		}
		seen[abs] = true

		data, err := os.ReadFile(abs)
		if err != nil {
			return fmt.Errorf("reading include %q: %w", inc.local, err)
		}
		resolved, err := resolve(data, filepath.Dir(abs), seen)
		if err != nil {
			return fmt.Errorf("resolving include %q: %w", inc.local, err)
		}

		var incDoc yaml.Node
		if err := yaml.Unmarshal(resolved, &incDoc); err != nil {
			return fmt.Errorf("parsing include %q: %w", inc.local, err)
		}
		if len(incDoc.Content) == 0 || incDoc.Content[0].Kind != yaml.MappingNode {
			continue
		}
		incRoot := incDoc.Content[0]
		mergeIncludedRoot(root, incRoot)
	}

	// remove the include key from the main document
	removeChildPair(root, idx)
	return nil
}

type includeRef struct {
	local string
}

func normalizeIncludes(node *yaml.Node) ([]includeRef, error) {
	switch node.Kind {
	case yaml.ScalarNode:
		return []includeRef{{local: node.Value}}, nil
	case yaml.SequenceNode:
		var out []includeRef
		for _, item := range node.Content {
			if item.Kind == yaml.ScalarNode {
				out = append(out, includeRef{local: item.Value})
			} else if item.Kind == yaml.MappingNode {
				ref, err := includeFromMapping(item)
				if err != nil {
					return nil, err
				}
				out = append(out, ref)
			} else {
				return nil, fmt.Errorf("include item must be a string or mapping")
			}
		}
		return out, nil
	case yaml.MappingNode:
		ref, err := includeFromMapping(node)
		if err != nil {
			return nil, err
		}
		return []includeRef{ref}, nil
	}
	return nil, fmt.Errorf("include must be a string, list or mapping")
}

func includeFromMapping(node *yaml.Node) (includeRef, error) {
	local := mappingValue(node, "local")
	if local != nil && local.Kind == yaml.ScalarNode {
		return includeRef{local: local.Value}, nil
	}
	return includeRef{}, nil
}

func resolveExtends(root *yaml.Node) {
	for i := 0; i < len(root.Content); i += 2 {
		key := root.Content[i].Value
		if reservedKeys[key] {
			continue
		}
		jobNode := root.Content[i+1]
		if jobNode.Kind != yaml.MappingNode {
			continue
		}
		extIdx := findMappingIndex(jobNode, "extends")
		if extIdx < 0 {
			continue
		}
		extVal := jobNode.Content[extIdx+1]
		sources := extendsNames(extVal)
		for _, src := range sources {
			srcNode := mappingValue(root, src)
			if srcNode == nil || srcNode.Kind != yaml.MappingNode {
				continue
			}
			mergeMapping(jobNode, srcNode)
		}
		removeChildPair(jobNode, extIdx)
	}
}

func extendsNames(node *yaml.Node) []string {
	if node.Kind == yaml.ScalarNode {
		return []string{node.Value}
	}
	if node.Kind != yaml.SequenceNode {
		return nil
	}
	var out []string
	for _, c := range node.Content {
		if c.Kind == yaml.ScalarNode {
			out = append(out, c.Value)
		}
	}
	return out
}

func expandReferences(node, root *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	if node.Tag == "!reference" {
		resolved, err := resolveReference(node, root)
		if err != nil {
			return node
		}
		return expandReferences(resolved, root)
	}
	switch node.Kind {
	case yaml.DocumentNode:
		for i, c := range node.Content {
			node.Content[i] = expandReferences(c, root)
		}
	case yaml.MappingNode:
		for i := 0; i < len(node.Content); i += 2 {
			node.Content[i] = expandReferences(node.Content[i], root)
			node.Content[i+1] = expandReferences(node.Content[i+1], root)
		}
	case yaml.SequenceNode:
		for i, c := range node.Content {
			node.Content[i] = expandReferences(c, root)
		}
	}
	return node
}

func resolveReference(ref, root *yaml.Node) (*yaml.Node, error) {
	if ref.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("!reference must be a sequence")
	}
	var path []string
	for _, c := range ref.Content {
		if c.Kind != yaml.ScalarNode {
			return nil, fmt.Errorf("!reference path must contain only scalars")
		}
		path = append(path, c.Value)
	}
	if len(path) < 2 {
		return nil, fmt.Errorf("!reference path must have at least two segments")
	}

	cur := root
	for _, p := range path {
		if cur.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("!reference cannot traverse non-mapping at %q", p)
		}
		next := mappingValue(cur, p)
		if next == nil {
			return nil, fmt.Errorf("!reference path %q not found", strings.Join(path, "."))
		}
		cur = next
	}
	return cloneNode(cur), nil
}

func mergeIncludedRoot(dst, src *yaml.Node) {
	for i := 0; i < len(src.Content); i += 2 {
		srcKey := src.Content[i].Value
		if srcKey == "include" {
			continue
		}
		srcVal := src.Content[i+1]
		dstIdx := findMappingIndex(dst, srcKey)
		if dstIdx < 0 {
			dst.Content = append(dst.Content, cloneNode(src.Content[i]), cloneNode(srcVal))
			continue
		}
		dstVal := dst.Content[dstIdx+1]
		if srcKey == "stages" && dstVal.Kind == yaml.SequenceNode && srcVal.Kind == yaml.SequenceNode {
			mergeStages(dstVal, srcVal)
			continue
		}
		if dstVal.Kind == yaml.MappingNode && srcVal.Kind == yaml.MappingNode {
			mergeMapping(dstVal, srcVal)
		}
	}
}

func mergeMapping(dst, src *yaml.Node) {
	for i := 0; i < len(src.Content); i += 2 {
		srcKey := src.Content[i].Value
		srcVal := src.Content[i+1]
		dstIdx := findMappingIndex(dst, srcKey)
		if dstIdx < 0 {
			dst.Content = append(dst.Content, cloneNode(src.Content[i]), cloneNode(srcVal))
			continue
		}
		dstVal := dst.Content[dstIdx+1]
		if dstVal.Kind == yaml.MappingNode && srcVal.Kind == yaml.MappingNode {
			mergeMapping(dstVal, srcVal)
		}
	}
}

func mergeStages(dst, src *yaml.Node) {
	seen := map[string]bool{}
	for _, c := range dst.Content {
		if c.Kind == yaml.ScalarNode {
			seen[c.Value] = true
		}
	}
	for _, c := range src.Content {
		if c.Kind == yaml.ScalarNode && !seen[c.Value] {
			dst.Content = append(dst.Content, cloneNode(c))
		}
	}
}

func mappingValue(m *yaml.Node, key string) *yaml.Node {
	idx := findMappingIndex(m, key)
	if idx < 0 {
		return nil
	}
	return m.Content[idx+1]
}

func findMappingIndex(m *yaml.Node, key string) int {
	if m.Kind != yaml.MappingNode {
		return -1
	}
	for i := 0; i < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return i
		}
	}
	return -1
}

func removeChildPair(m *yaml.Node, idx int) {
	if m.Kind != yaml.MappingNode || idx < 0 || idx+1 >= len(m.Content) {
		return
	}
	m.Content = append(m.Content[:idx], m.Content[idx+2:]...)
}

func cloneNode(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	c := &yaml.Node{
		Kind:        n.Kind,
		Tag:         n.Tag,
		Value:       n.Value,
		Style:       n.Style,
		HeadComment: n.HeadComment,
		LineComment: n.LineComment,
		FootComment: n.FootComment,
		Line:        n.Line,
		Column:      n.Column,
		Content:     make([]*yaml.Node, len(n.Content)),
	}
	for i, child := range n.Content {
		c.Content[i] = cloneNode(child)
	}
	return c
}
