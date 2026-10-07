package render

import (
	"strings"
)

// Final is the closing block of setup and offboard: what changed, what the
// person still must do, and where the log is.
type Final struct {
	Changed []string    // one short sentence each, imperative or past tense
	Backups [][2]string // what was saved, and the path it was saved to
	Todo    []FinalTodo
	LogPath string
}

// FinalTodo is one ACTION line, with an optional command to copy.
type FinalTodo struct{ Text, Cmd string }

// Backup is the line that names a backup path before a step replaces or removes
// something.
func Backup(s Style, what, path string) string {
	return Note(s, "backup: "+what+" is saved to "+path)
}

// FinalBlock renders f. It writes nothing for an empty Final.
func FinalBlock(s Style, f Final) string {
	var b strings.Builder
	if len(f.Changed) > 0 {
		b.WriteString(Section(s, "What changed"))
		for _, c := range f.Changed {
			b.WriteString(Note(s, "  "+c))
		}
	}
	for _, bk := range f.Backups {
		b.WriteString(Backup(s, bk[0], bk[1]))
	}
	for _, t := range f.Todo {
		b.WriteString(Action(s, t.Text))
		if t.Cmd != "" {
			b.WriteString(Cmd(s, t.Cmd))
		}
	}
	if f.LogPath != "" {
		b.WriteString(KV(s, "log", f.LogPath))
	}
	return b.String()
}

// Final writes the closing block.
func (w Writer) Final(f Final) { w.put(FinalBlock(w.S, f)) }

// AutoYes answers an undoable step (DefaultYes) yes under --yes and shows the
// question with that answer. It reports whether it answered; a step that cannot
// be undone (DefaultNo) is never answered here.
func AutoYes(w Writer, question string, d Default, yes bool) (Answer, bool) {
	if yes && d == DefaultYes {
		w.Note("yes: " + question)
		return Yes, true
	}
	return No, false
}

// Preflight is the one line before the first change: the tools that were found,
// the tools that are missing, what exists already, and what will happen.
func Preflight(s Style, found, missing []string, state, will string) string {
	list := func(l []string) string {
		if len(l) == 0 {
			return "none"
		}
		return strings.Join(l, ", ")
	}
	return Note(s, "preflight: tools found: "+list(found)+"; missing: "+
		list(missing)+". State: "+state+". Next: "+will)
}
