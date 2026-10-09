package agent

// ImageTokens is what one image is assumed to cost.
const ImageTokens = 1600

// EstimateTokens guesses how many tokens msgs take in a request: the usage
// the newest reply reported for everything up to it, plus about one token
// per four characters of what came after (or of everything, without one).
func EstimateTokens(msgs []Message) int {
	start, total := 0, 0
	for i := len(msgs) - 1; i >= 0; i-- {
		if m := msgs[i]; m.Role == RoleAssistant && m.Usage.Context() > 0 {
			start, total = i+1, m.Usage.Context()
			break
		}
	}
	for _, m := range msgs[start:] {
		total += MessageTokens(m)
	}
	return total
}

// MessageTokens is the four-characters-a-token guess for one message.
func MessageTokens(m Message) int {
	n := 4 // the message's own framing
	for _, p := range m.Parts {
		switch p.Kind {
		case PartImage:
			n += ImageTokens
		default:
			n += (len(p.Text) + len(p.Args) + len(p.Raw) + len(p.Signature)/2 + len(p.Name)) / 4
		}
	}
	return n
}

// TextTokens is the guess for a string.
func TextTokens(s string) int { return len(s) / 4 }
