package supervisor

import (
	"bytes"
	"sort"
	"strconv"
)

// stickyModes are DEC private modes the harness enables on the client terminal
// and expects to persist for the session — mouse tracking, bracketed paste,
// focus reporting. vt10x does not track these, so the supervisor records them
// from the output stream and replays them to a newly attached client (whose
// terminal was reset on the previous detach).
var stickyModes = map[int]bool{
	1000: true, 1002: true, 1003: true, // mouse: click, drag, any-motion
	1006: true, 1015: true, // mouse encodings: SGR, urxvt
	2004: true, // bracketed paste
	1004: true, // focus reporting
}

const kittyKeyboardStackLimit = 64

// observeModes updates enabled from the private-mode set/reset sequences
// (ESC [ ? n[;n...] h|l) present in p.
func observeModes(enabled map[int]bool, p []byte) {
	for i := 0; i+3 < len(p); i++ {
		if p[i] != 0x1b || p[i+1] != '[' || p[i+2] != '?' {
			continue
		}
		j := i + 3
		for j < len(p) && (p[j] >= '0' && p[j] <= '9' || p[j] == ';') {
			j++
		}
		if j >= len(p) || (p[j] != 'h' && p[j] != 'l') {
			continue
		}
		set := p[j] == 'h'
		for _, n := range parseParams(p[i+3 : j]) {
			if stickyModes[n] {
				if set {
					enabled[n] = true
				} else {
					delete(enabled, n)
				}
			}
		}
		i = j
	}
}

type kittyKeyboardState struct {
	stack   []int
	current int
}

// observeKittyKeyboard records Kitty keyboard protocol state changes from
// ESC [ > flags u (push), ESC [ = flags u (set), and ESC [ < [n] u (pop).
func observeKittyKeyboard(state *kittyKeyboardState, p []byte) {
	for i := 0; i < len(p); i++ {
		cmd, params, end, ok := scanKittyKeyboardSequence(p, i)
		if !ok {
			continue
		}
		switch cmd {
		case '>':
			state.push(kittyKeyboardParam(params, 0))
		case '=':
			flags, mode := kittyKeyboardSetParams(params)
			state.set(flags, mode)
		case '<':
			state.pop(kittyPopCount(params))
		}
		i = end - 1
	}
}

func (s *kittyKeyboardState) push(flags int) {
	if len(s.stack) == kittyKeyboardStackLimit {
		copy(s.stack, s.stack[1:])
		s.stack[len(s.stack)-1] = s.current
	} else {
		s.stack = append(s.stack, s.current)
	}
	s.current = flags
}

func (s *kittyKeyboardState) set(flags, mode int) {
	switch mode {
	case 2:
		s.current |= flags
	case 3:
		s.current &^= flags
	default:
		s.current = flags
	}
}

func (s *kittyKeyboardState) pop(n int) {
	if n >= len(s.stack) {
		s.stack = nil
		s.current = 0
		return
	}
	for ; n > 0; n-- {
		last := len(s.stack) - 1
		s.current = s.stack[last]
		s.stack = s.stack[:last]
	}
}

// sequence returns the escape bytes that reconstruct the current Kitty keyboard
// flag stack on a freshly reset attach client.
func (s kittyKeyboardState) sequence() []byte {
	if len(s.stack) == 0 {
		if s.current == 0 {
			return nil
		}
		return appendKittyKeyboardSequence(nil, '=', s.current)
	}

	var out []byte
	if s.stack[0] != 0 {
		out = appendKittyKeyboardSequence(out, '=', s.stack[0])
	}
	for _, flags := range s.stack[1:] {
		out = appendKittyKeyboardSequence(out, '>', flags)
	}
	return appendKittyKeyboardSequence(out, '>', s.current)
}

func kittyKeyboardResetSequence() []byte {
	return []byte("\x1b[<64u\x1b[=0;1u")
}

func appendKittyKeyboardSequence(out []byte, cmd byte, flags int) []byte {
	out = append(out, "\x1b["...)
	out = append(out, cmd)
	out = strconv.AppendInt(out, int64(flags), 10)
	return append(out, 'u')
}

func stripKittyKeyboardSequences(p []byte) []byte {
	var out []byte
	last := 0
	for i := 0; i < len(p); i++ {
		_, _, end, ok := scanKittyKeyboardSequence(p, i)
		if !ok {
			continue
		}
		out = append(out, p[last:i]...)
		last = end
		i = end - 1
	}
	if out == nil {
		return p
	}
	return append(out, p[last:]...)
}

func scanKittyKeyboardSequence(p []byte, i int) (cmd byte, params []byte, end int, ok bool) {
	if i+3 >= len(p) || p[i] != 0x1b || p[i+1] != '[' {
		return 0, nil, 0, false
	}
	cmd = p[i+2]
	if cmd != '>' && cmd != '=' && cmd != '<' {
		return 0, nil, 0, false
	}
	j := i + 3
	for j < len(p) && (p[j] >= '0' && p[j] <= '9' || p[j] == ';') {
		j++
	}
	if j >= len(p) || p[j] != 'u' {
		return 0, nil, 0, false
	}
	return cmd, p[i+3 : j], j + 1, true
}

func kittyKeyboardSetParams(params []byte) (flags, mode int) {
	flagsPart, modePart, hasMode := bytes.Cut(params, []byte{';'})
	flags = kittyKeyboardParam(flagsPart, 0)
	if !hasMode {
		return flags, 1
	}
	return flags, kittyKeyboardParam(modePart, 1)
}

func kittyKeyboardParam(params []byte, fallback int) int {
	if len(params) == 0 {
		return fallback
	}
	n, err := strconv.Atoi(string(params))
	if err != nil || n < 0 {
		return fallback
	}
	return n
}

func kittyPopCount(params []byte) int {
	count := kittyKeyboardParam(params, 1)
	if count <= 0 {
		return 1
	}
	return count
}

// modeSequence returns the escape bytes that re-enable the currently sticky
// modes, in stable numeric order.
func modeSequence(enabled map[int]bool) []byte {
	if len(enabled) == 0 {
		return nil
	}
	nums := make([]int, 0, len(enabled))
	for n := range enabled {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	var out []byte
	for _, n := range nums {
		out = append(out, "\x1b[?"...)
		out = append(out, strconv.Itoa(n)...)
		out = append(out, 'h')
	}
	return out
}

func parseParams(b []byte) []int {
	var out []int
	cur, has := 0, false
	for _, c := range b {
		switch {
		case c >= '0' && c <= '9':
			cur = cur*10 + int(c-'0')
			has = true
		case c == ';':
			if has {
				out = append(out, cur)
			}
			cur, has = 0, false
		}
	}
	if has {
		out = append(out, cur)
	}
	return out
}
