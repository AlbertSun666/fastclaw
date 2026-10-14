package setup

import (
	"fmt"
	"github.com/fastclaw-ai/fastclaw/internal/replytext"
	"golang.org/x/text/unicode/norm"
	"strings"
	"time"
)

type teamPrivateMessage struct {
	ID        string `json:"id"`
	Sender    string `json:"sender"`
	Recipient string `json:"recipient"`
	Content   string `json:"content,omitempty"`
	// Intent "inform" delivers without scheduling the recipient to reply
	// ([[private-info:...]]); empty is an ordinary request.
	Intent    string `json:"intent,omitempty"`
	Timestamp int64  `json:"timestamp"`
	// Session is the sender's direct chat a human-addressed message was
	// delivered into, so the unread notice can open it.
	Session string `json:"session,omitempty"`
}
type privateBlock struct{ to, content, intent string }

// Withhold even an incomplete opening marker. Neither unknown destinations nor
// malformed/private-only replies may leak into public streaming or history.
func parseTeamPrivate(text string) (string, []privateBlock, bool) {
	lower := strings.ToLower(text)
	const open = "[[private:"
	// [[private-info:X]] is information-only: delivered, never a request.
	const infoOpen = "[[private-info:"
	const close = "[[/private]]"
	var public strings.Builder
	var blocks []privateBlock
	valid := true
	for cursor := 0; cursor < len(text); {
		relative := strings.Index(lower[cursor:], "[[private")
		if relative < 0 {
			tail := text[cursor:]
			held := 0
			for size := 1; size < len(open); size++ {
				if strings.HasSuffix(strings.ToLower(tail), open[:size]) {
					held = size
				}
			}
			public.WriteString(tail[:len(tail)-held])
			if held > 0 {
				valid = false
			}
			break
		}
		start := cursor + relative
		public.WriteString(text[cursor:start])
		h := strings.Index(lower[start:], "]]")
		if h < 0 {
			valid = false
			break
		}
		h += start
		e := strings.Index(lower[h+2:], close)
		if e < 0 {
			valid = false
			break
		}
		e += h + 2
		content := strings.TrimSpace(text[h+2 : e])
		header, intent := open, ""
		if strings.HasPrefix(lower[start:], infoOpen) {
			header, intent = infoOpen, "inform"
		}
		if !strings.HasPrefix(lower[start:], header) || content == "" || strings.Contains(strings.ToLower(content), "[[private") || len([]rune(content)) > 12000 || len(blocks) >= 20 {
			valid = false
		} else {
			blocks = append(blocks, privateBlock{strings.TrimSpace(text[start+len(header) : h]), content, intent})
		}
		cursor = e + len(close)
	}
	return public.String(), blocks, valid
}
func resolveTeamPrivate(text string, sender resolvedTeamMember, members []resolvedTeamMember, id string) ([]teamPrivateMessage, error) {
	_, blocks, valid := parseTeamPrivate(text)
	if !valid {
		return nil, fmt.Errorf("私信格式不完整，未发送")
	}
	var result []teamPrivateMessage
	for i, b := range blocks {
		recipient := ""
		if b.to == "human" {
			recipient = "human"
		} else {
			for _, m := range members {
				if b.to == m.AgentID || strings.EqualFold(norm.NFKC.String(b.to), norm.NFKC.String(m.Name)) {
					if recipient != "" {
						return nil, fmt.Errorf("私信收件人不唯一")
					}
					recipient = m.AgentID
				}
			}
		}
		if recipient == "" || recipient == sender.AgentID {
			return nil, fmt.Errorf("私信收件人无效，未发送")
		}
		result = append(result, teamPrivateMessage{ID: fmt.Sprintf("%s-private-%d", id, i), Sender: sender.AgentID, Recipient: recipient, Content: b.content, Intent: b.intent, Timestamp: time.Now().UnixMilli()})
	}
	return result, nil
}

// Message separators may appear anywhere in prose, but code is literal text.
func splitTeamReply(text string) []string {
	return splitTeamReplyText(text, false)
}

func splitTeamReplyText(text string, streaming bool) []string {
	return replytext.Split(text, streaming)
}
