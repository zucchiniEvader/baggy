package agent

import "slices"

const (
	NonVisionUserImage = "(image omitted: model does not support images)"
	NonVisionToolImage = "(tool image omitted: model does not support images)"
	NoResultProvided   = "No result provided"
	OlderImageOmitted  = "(older image omitted: a request carries only the latest images)"

	// MaxRequestImages and MaxRequestImageBytes (of base64) bound the images
	// one request carries; older ones become OlderImageOmitted.
	MaxRequestImages     = 20
	MaxRequestImageBytes = 16 << 20
)

// TransformOptions describe the model a request goes to.
type TransformOptions struct {
	// Provider and Model identify the target. An assistant message from the
	// same pair is "own": its thinking seals and server blocks are kept and
	// its tool call ids left alone.
	Provider, Model string
	SupportsImages  bool
	// NormalizeToolCallID rewrites a tool call id from another model into the
	// shape this API accepts; the matching results follow. Nil leaves ids be.
	NormalizeToolCallID func(string) string
}

// Transform is what every adapter does to the transcript before converting
// it, after pi's transformMessages:
//
//   - images a model cannot see become a note;
//   - another model's thinking becomes plain text, and its server blocks go;
//   - tool call ids from another model get the shape the API wants;
//   - failed and aborted turns are left out;
//   - a tool call that never got a result gets an error result before the
//     next assistant turn, the next user message, or the end;
//   - only the latest images go (limitImages).
//
// The input is not modified. origins[i] is the index in msgs that out[i]
// comes from; a made-up result belongs to the message it follows.
func Transform(msgs []Message, o TransformOptions) (out []Message, origins []int) {
	type rename struct{ from, to string }
	var ids []rename

	prepared := make([]Message, len(msgs))
	for i, m := range msgs {
		m.Parts = slices.Clone(m.Parts)
		switch m.Role {
		case RoleAssistant:
			own := m.Provider == o.Provider && m.Model == o.Model
			parts := m.Parts[:0]
			for _, p := range m.Parts {
				switch p.Kind {
				case PartThinking:
					switch {
					case own && p.Signature != "":
						parts = append(parts, p)
					case trimmedEmpty(p.Text): // nothing to say and no seal to keep
					case own:
						parts = append(parts, p)
					default:
						parts = append(parts, Part{Kind: PartText, Text: p.Text})
					}
				case PartServer:
					if own {
						parts = append(parts, p)
					}
				case PartToolCall:
					if !own && o.NormalizeToolCallID != nil {
						if id := o.NormalizeToolCallID(p.CallID); id != p.CallID {
							ids = append(ids, rename{p.CallID, id})
							p.CallID = id
						}
					}
					parts = append(parts, p)
				default:
					parts = append(parts, p)
				}
			}
			m.Parts = parts
		default:
			if !o.SupportsImages {
				note := NonVisionUserImage
				if m.Role == RoleTool {
					note = NonVisionToolImage
				}
				m.Parts = replaceImages(m.Parts, note)
			}
			if m.Role == RoleTool {
				for j, p := range m.Parts {
					if p.Kind != PartToolResult {
						continue
					}
					for _, r := range ids {
						if r.from == p.CallID {
							m.Parts[j].CallID = r.to
							break
						}
					}
				}
			}
		}
		prepared[i] = m
	}

	var pending []Part // calls of the last kept assistant message
	var answered []string
	settle := func() {
		var missing []Part
		for _, c := range pending {
			if !slices.Contains(answered, c.CallID) {
				missing = append(missing, Part{Kind: PartToolResult, CallID: c.CallID, Name: c.Name, Text: NoResultProvided, IsError: true})
			}
		}
		if len(missing) > 0 {
			origin := 0
			if n := len(origins); n > 0 {
				origin = origins[n-1]
			}
			out = append(out, Message{Role: RoleTool, Parts: missing})
			origins = append(origins, origin)
		}
		pending, answered = nil, nil
	}
	for i, m := range prepared {
		switch m.Role {
		case RoleAssistant:
			settle()
			if m.StopReason == StopError || m.StopReason == StopAborted {
				continue
			}
			for _, p := range m.Parts {
				if p.Kind == PartToolCall {
					pending = append(pending, p)
				}
			}
		case RoleTool:
			for _, p := range m.Parts {
				if p.Kind == PartToolResult {
					answered = append(answered, p.CallID)
				}
			}
		default:
			settle()
		}
		out = append(out, m)
		origins = append(origins, i)
	}
	settle()
	limitImages(out)
	return out, origins
}

// limitImages keeps the latest MaxRequestImages images, and as many of them
// as fit MaxRequestImageBytes of base64, never dropping the newest. Past the
// count they go ten at a time, so the same ones stay out from one request to
// the next and the cached prefix breaks once in ten images.
func limitImages(msgs []Message) {
	type at struct{ m, p, size int }
	var imgs []at
	for i, m := range msgs {
		for j, p := range m.Parts {
			if p.Kind == PartImage {
				imgs = append(imgs, at{i, j, (len(p.Data) + 2) / 3 * 4})
			}
		}
	}
	drop := 0
	if over := len(imgs) - MaxRequestImages; over > 0 {
		drop = (over + 9) / 10 * 10
	}
	total := 0
	for _, im := range imgs[drop:] {
		total += im.size
	}
	for drop < len(imgs)-1 && total > MaxRequestImageBytes {
		total -= imgs[drop].size
		drop++
	}
	for _, im := range imgs[:min(drop, len(imgs))] {
		msgs[im.m].Parts[im.p] = Part{Kind: PartText, Text: OlderImageOmitted}
	}
}

// PrefixLen maps a prefix length of the original transcript to the same
// prefix of the transformed one, given the origins Transform returned.
func PrefixLen(origins []int, n int) int {
	k := 0
	for _, o := range origins {
		if o < n {
			k++
		}
	}
	return k
}

func trimmedEmpty(s string) bool {
	for _, r := range s {
		if r != ' ' && r != '\t' && r != '\n' && r != '\r' {
			return false
		}
	}
	return true
}

// replaceImages swaps each run of images for one note.
func replaceImages(parts []Part, note string) []Part {
	out := make([]Part, 0, len(parts))
	prevNote := false
	for _, p := range parts {
		if p.Kind == PartImage {
			if !prevNote {
				out = append(out, Part{Kind: PartText, Text: note})
			}
			prevNote = true
			continue
		}
		prevNote = p.Kind == PartText && p.Text == note
		out = append(out, p)
	}
	return out
}
