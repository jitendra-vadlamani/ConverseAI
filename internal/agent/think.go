package agent

import "strings"

const (
	openTag  = "<think>"
	closeTag = "</think>"
)

// thinkSplitter separates <think>...</think> reasoning from answer text in a
// stream, even when a tag is split across chunks.
type thinkSplitter struct {
	inThink bool
	pending string // tail that might be the start of a tag
	answer  strings.Builder
	thought strings.Builder
}

// Feed consumes a chunk and returns the answer and thought text that is now
// safe to emit.
func (s *thinkSplitter) Feed(chunk string) (answer, thought string) {
	buf := s.pending + chunk
	s.pending = ""
	var a, t strings.Builder
	for buf != "" {
		tag := openTag
		if s.inThink {
			tag = closeTag
		}
		if i := strings.Index(buf, tag); i >= 0 {
			s.emit(&a, &t, buf[:i])
			buf = buf[i+len(tag):]
			s.inThink = !s.inThink
			continue
		}
		// Hold back a suffix that could be the beginning of the tag.
		keep := partialSuffix(buf, tag)
		s.emit(&a, &t, buf[:len(buf)-keep])
		s.pending = buf[len(buf)-keep:]
		break
	}
	return a.String(), t.String()
}

// Flush returns whatever was held back at the end of the stream.
func (s *thinkSplitter) Flush() (answer, thought string) {
	var a, t strings.Builder
	s.emit(&a, &t, s.pending)
	s.pending = ""
	return a.String(), t.String()
}

func (s *thinkSplitter) emit(a, t *strings.Builder, text string) {
	if text == "" {
		return
	}
	if s.inThink {
		t.WriteString(text)
		s.thought.WriteString(text)
	} else {
		a.WriteString(text)
		s.answer.WriteString(text)
	}
}

func (s *thinkSplitter) Answer() string  { return s.answer.String() }
func (s *thinkSplitter) Thought() string { return s.thought.String() }

// partialSuffix is the length of the longest suffix of s that is a proper
// prefix of tag.
func partialSuffix(s, tag string) int {
	for n := min(len(tag)-1, len(s)); n > 0; n-- {
		if strings.HasSuffix(s, tag[:n]) {
			return n
		}
	}
	return 0
}
