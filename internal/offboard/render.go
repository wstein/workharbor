package offboard

import (
	"fmt"
	"io"
	"strings"

	"github.com/wstein/workharbor/internal/doctor"
	"github.com/wstein/workharbor/internal/render"
	"github.com/wstein/workharbor/internal/setup"
	"github.com/wstein/workharbor/internal/textsafe"
)

// Out is every way this package writes for a person: human text on Err, data
// on Out. All rendering goes through the methods below, which use the shared
// vocabulary of internal/render (#320). The zero Style is plain ASCII.
type Out struct {
	Out, Err io.Writer
	Style    render.Style
}

func (o Out) ui() render.Writer { return render.Writer{W: o.Err, S: o.Style} }

// Report writes one outcome line (ok, fail, not verified) for the person.
func (o Out) Report(l render.Level, text string) { o.ui().Report(l, plain(text)) }

func plain(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || textsafe.IsControl(r) || r < 0x20 || textsafe.IsBidiOrSeparator(r) {
			return ' '
		}
		return r
	}, s)
}

// Data writes one tab-separated line to stdout.
func (o Out) Data(fields ...string) {
	for i, f := range fields {
		fields[i] = plain(f)
	}
	fmt.Fprintln(o.Out, strings.Join(fields, "\t"))
}

// Note writes a line of human text.
func (o Out) Note(format string, a ...any) { fmt.Fprintf(o.Err, format+"\n", a...) }

// Action says what a step does before it runs.
func (o Out) Action(text string) { o.ui().Action(plain(text)) }

// Command shows an argument vector, never run through a shell.
func (o Out) Command(c doctor.Cmd) { o.ui().Command(setup.QuoteArgv(c.Full())) }

// Refusal writes one refused guard.
func (o Out) Refusal(r Refusal) {
	o.ui().Report(render.LevelFail, plain(fmt.Sprintf("refused (%s): %s", r.Guard, r.Msg)))
}
