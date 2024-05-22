package yaml

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/hashicorp/go-multierror"
	goyaml "gopkg.in/yaml.v3"
)

type Processor struct {
	node *goyaml.Node
}

func NewProcessor(r io.Reader) (*Processor, error) {
	dec := goyaml.NewDecoder(r)
	var node goyaml.Node
	if err := dec.Decode(&node); err != nil {
		return nil, err
	}

	return &Processor{
		node: &node,
	}, nil
}

func (p *Processor) Encode(w io.Writer) error {
	enc := goyaml.NewEncoder(w)
	enc.SetIndent(2)
	return enc.Encode(p.node)
}

type RebaseAnnotation struct {
	Identifier string
	Name       string
	Tag        string

	nameNode *goyaml.Node
	tagNode  *goyaml.Node
}

func (a RebaseAnnotation) TagWithoutDigest() string {
	tag := a.Tag

	idx := strings.IndexByte(tag, '@')
	if idx >= 0 {
		tag = tag[:idx]
	}

	return tag
}

// UpdateTagDigest updates the YAML node for the tag with a new digest.
func (a *RebaseAnnotation) UpdateTagDigest(newDigest string) {
	a.tagNode.SetString(a.TagWithoutDigest() + "@" + newDigest)
}

type commentAnnotation map[string]string

func (p *Processor) FindRebaseAnnotations() ([]RebaseAnnotation, error) {
	var annotations map[string]*RebaseAnnotation
	var visitErr error

	p.visitMappingScalarNodes(p.node, func(node *goyaml.Node) {
		comment := node.LineComment
		if comment == "" {
			return
		}

		// Strip leading comment character and whitespace
		comment = strings.TrimLeft(comment, "#")
		comment = strings.TrimSpace(comment)

		if !strings.HasPrefix(comment, "{") {
			return
		}

		// Try to parse JSON
		var comAnt commentAnnotation
		err := json.Unmarshal([]byte(comment), &comAnt)
		if err != nil {
			visitErr = multierror.Append(visitErr, fmt.Errorf("parsing JSON from annotation in line %d: %w", node.Line, err))
			return
		}

		if _, exists := comAnt["$rebase"]; !exists {
			slog.Debug("Ignoring annotation", "annotation", comment, "line", node.Line)
			return
		}

		rebaseValue := comAnt["$rebase"]
		rebaseIdentifierAndPart := strings.Split(rebaseValue, ":")
		if len(rebaseIdentifierAndPart) != 2 {
			visitErr = multierror.Append(visitErr, fmt.Errorf("invalid value %q in $rebase annotation of line %d", rebaseValue, node.Line))
			return
		}

		identifier := rebaseIdentifierAndPart[0]
		part := rebaseIdentifierAndPart[1]

		if annotations == nil {
			annotations = make(map[string]*RebaseAnnotation)
		}

		annotation := annotations[identifier]
		if annotation == nil {
			annotation = &RebaseAnnotation{
				Identifier: identifier,
			}
			annotations[identifier] = annotation
		}

		switch part {
		case "name":
			annotation.Name = node.Value
			annotation.nameNode = node
		case "tag":
			annotation.Tag = node.Value
			annotation.tagNode = node
		default:
			visitErr = multierror.Append(visitErr, fmt.Errorf("invalid part %q in $rebase annotation of line %d, expected \"name\" or \"tag\"", part, node.Line))
			return
		}
	})

	var result []RebaseAnnotation
	for _, annotation := range annotations {
		result = append(result, *annotation)
	}

	return result, visitErr
}

func (p *Processor) visitMappingScalarNodes(node *goyaml.Node, f func(node *goyaml.Node)) {
	if node.Kind == goyaml.DocumentNode {
		p.visitMappingScalarNodes(node.Content[0], f)
	}

	if node.Kind == goyaml.MappingNode {
		for i := 0; i < len(node.Content); i += 2 {
			value := node.Content[i+1]
			p.visitMappingScalarNodes(value, f)
		}
	}

	if node.Kind == goyaml.ScalarNode {
		f(node)
	}
}
