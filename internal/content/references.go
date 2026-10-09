package content

import (
	"context"
	"fmt"
	"strings"
)

// Check the largest legal continuation offset before issuing any reference.
// JSON escaping can expand otherwise short source identifiers considerably.
func referenceFits(part, member, memberHash string) bool {
	c := cursor{SchemaVersion: 1, AssetID: "sha256:" + strings.Repeat("0", 64), Extractor: structuredExtractor, RepresentationID: "sha256:" + strings.Repeat("0", 64), Unit: maxUnits - 1, Offset: maxText, Part: part, Representation: "epub-structure/1", Member: member, MemberSHA256: memberHash}
	return len(encodeCursor(c)) <= 2048
}

func resolveLinks(ctx context.Context, docs []document) error {
	members := map[string]int{}
	for i, d := range docs {
		members[d.unit.Member] = i
	}
	graph := map[string][]string{}
	sources := map[*Link]string{}
	node := func(unit int, part string) string { return fmt.Sprintf("%d/%s", unit, part) }
	for unit, d := range docs {
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, p := range d.parts {
			graph[node(unit, p.Parent)] = append(graph[node(unit, p.Parent)], node(unit, p.ID))
		}
		for _, p := range d.parts {
			link := p.Link
			if link == nil {
				continue
			}
			member, fragment, status := localReference(d.unit.Member, link.Href)
			link.Status = status
			if status != "local" {
				continue
			}
			target, ok := members[member]
			if !ok {
				link.Status = "outside_reading_order"
				continue
			}
			link.TargetUnit = target
			link.Status = "resolved"
			if fragment != "" {
				link.TargetPart = "id:" + fragment
				found := false
				for _, part := range docs[target].parts {
					if part.ID == link.TargetPart {
						found = true
					}
				}
				if !found {
					link.Status = "missing_fragment"
					continue
				}
			}
			sources[link] = node(unit, p.ID)
			graph[node(unit, p.ID)] = append(graph[node(unit, p.ID)], node(target, link.TargetPart))
		}
	}
	// Detect cycles without following source links or recursively expanding notes.
	// The graph and total parts are bounded by extraction limits.
	for link, source := range sources {
		queue := []string{node(link.TargetUnit, link.TargetPart)}
		seen := map[string]bool{}
		for len(queue) > 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
			current := queue[0]
			queue = queue[1:]
			if current == source {
				link.Cyclic = true
				break
			}
			if seen[current] {
				continue
			}
			seen[current] = true
			queue = append(queue, graph[current]...)
		}
	}
	return nil
}

func bindReferences(docs []document, assetID, representationID string) {
	for unit, d := range docs {
		for i := range d.parts {
			p := &d.parts[i]
			p.Locator = &Locator{SchemaVersion: 1, AssetID: assetID, Extractor: structuredExtractor, RepresentationID: representationID, Unit: unit, Start: p.Start, End: p.End, Member: d.unit.Member, OffsetUnit: "utf8_bytes"}
			base := cursor{SchemaVersion: 1, AssetID: assetID, Extractor: structuredExtractor, RepresentationID: representationID, Unit: unit, Offset: p.Start, Representation: "epub-structure/1", Part: p.ID}
			p.Reference = encodeCursor(base)
			base.Representation, base.Offset = "epub-source/1", p.SourceStart
			p.SourceReference = encodeCursor(base)
			if p.Link != nil && p.Link.Status == "resolved" {
				base.Unit, base.Part, base.Offset, base.Representation = p.Link.TargetUnit, p.Link.TargetPart, 0, "epub-structure/1"
				for _, target := range docs[base.Unit].parts {
					if target.ID == base.Part {
						base.Offset = target.Start
					}
				}
				p.Link.Reference = encodeCursor(base)
			}
			if p.Image != nil && p.Image.Status == "local_resource" {
				base.Unit, base.Part, base.Offset, base.Representation = unit, p.ID, 0, "epub-structure/1"
				base.Member, base.MemberSHA256 = p.Image.Member, p.Image.SHA256
				p.Image.Reference = encodeCursor(base)
			}
		}
	}
}
