package config

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

// RegisterDependencies validates a complete candidate before the caller writes it.
// Local switches, labels, unrelated declarations and top-level settings survive.
func (c *Config) RegisterDependencies(original []byte, incoming []Dependency, update bool) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(original, &doc); err != nil {
		return nil, err
	}
	root := doc.Content[0]
	var sequence *yaml.Node
	for i := 0; i < len(root.Content); i += 2 {
		if root.Content[i].Value == "dependencies" {
			sequence = root.Content[i+1]
		}
	}
	if sequence == nil {
		sequence = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "dependencies"}, sequence)
	}
	if sequence.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("dependencies must be an explicit YAML list to register assets")
	}
	if len(sequence.Content) != len(c.Dependencies) {
		return nil, fmt.Errorf("dependencies inherited through YAML merges must be made explicit before registration")
	}
	known := map[string]int{}
	for i, d := range c.Dependencies {
		known[d.Name] = i
	}
	changed := false
	for _, d := range incoming {
		if index, ok := known[d.Name]; ok {
			old := c.Dependencies[index]
			if old.Source == d.Source && old.Ref == d.Ref && old.Subdir == d.Subdir && old.MirrorDir == d.MirrorDir {
				continue
			}
			if !update {
				return nil, fmt.Errorf("configuration conflict for %s:\nlocal:  source=%q ref=%q subdir=%q mirror_dir=%q\nremote: source=%q ref=%q subdir=%q mirror_dir=%q\nreview both and retry with --update-config to accept remote identity (local switches and labels are preserved)", d.Name, old.Source, old.Ref, old.Subdir, old.MirrorDir, d.Source, d.Ref, d.Subdir, d.MirrorDir)
			}
			entry := sequence.Content[index]
			if entry.Kind != yaml.MappingNode || entry.Anchor != "" {
				return nil, fmt.Errorf("%s must use an explicit YAML mapping without anchors/aliases to update its identity", d.Name)
			}
			// Replace identity fields only. Keep local booleans/labels and comments.
			fields := []struct{ key, value string }{{"source", d.Source}, {"ref", d.Ref}, {"subdir", d.Subdir}, {"mirror_dir", d.MirrorDir}}
			for _, field := range fields {
				found := false
				for i := 0; i < len(entry.Content); i += 2 {
					key := entry.Content[i]
					if key.Value == field.key || (field.key == "ref" && key.Value == "version") {
						value := entry.Content[i+1]
						if key.Kind != yaml.ScalarNode || key.Anchor != "" || value.Kind != yaml.ScalarNode || value.Anchor != "" {
							return nil, fmt.Errorf("%s identity field %s uses a YAML anchor/alias; make identity fields explicit before --update-config to preserve unrelated declarations", d.Name, key.Value)
						}
						key.Value = field.key
						entry.Content[i+1].Value, entry.Content[i+1].Tag = field.value, "!!str"
						found = true
					}
				}
				if !found {
					entry.Content = append(entry.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: field.key}, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: field.value})
				}
			}
		} else {
			var node yaml.Node
			if err := node.Encode(d); err != nil {
				return nil, err
			}
			sequence.Content = append(sequence.Content, &node)
		}
		changed = true
	}
	if !changed {
		return original, nil
	}
	sequence.Style = 0
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(&doc); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	candidate, err := Parse(out.Bytes())
	if err != nil {
		return nil, fmt.Errorf("registered configuration invalid: %w", err)
	}
	candidate.Path = c.Path
	if err := candidate.Resolve(); err != nil {
		return nil, fmt.Errorf("registered configuration conflicts with local paths/settings: %w", err)
	}
	return out.Bytes(), nil
}
