// SPDX-License-Identifier: EUPL-1.2

//go:build linux || darwin

package ptytest_test

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/ptytest"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/terminal"
)

var fixtures = map[string]func(args []string) int{
	"echo":         fixtureEcho,
	"keys":         fixtureKeys,
	"env":          fixtureEnv,
	"winch":        fixtureWinch,
	"ctrlc72":      fixtureCtrlC,
	"backcancel":   fixtureBackCancel,
	"hang":         fixtureHang,
	"spawn":        fixtureSpawn,
	"hostile-raw":  fixtureHostileRaw,
	"hostile-safe": fixtureHostileSafe,
	"colored":      fixtureColored,
	"redraw":       fixtureRedraw,
	"winch-wide":   fixtureWinchWide,
	"floodseq":     fixtureFloodSeq,
	"split":        fixtureSplit,
	"partialosc":   fixturePartialOSC,
	"late":         fixtureLate,
	"flood":        fixtureFlood,
	"emit":         fixtureEmit,
	"envdump":      fixtureEnvDump,
	"pwd":          fixturePwd,
	"selfkill":     fixtureSelfKill,
	"quiet":        fixtureQuiet,
	"exitfast":     func([]string) int { return 3 },
}

const hostileText = "evil\x1b]0;pwned\x07\x1b[2J\u009b31m\x07end"

func fixtureEcho([]string) int {
	fmt.Print("Name: ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	fmt.Printf("Hello, %s\n", strings.TrimSpace(line))
	return 0
}

func fixtureKeys([]string) int {
	restore, err := ptytest.MakeRaw(os.Stdin.Fd())
	if err != nil {
		fmt.Println(err)
		return 1
	}
	defer restore()
	fmt.Print("ready\r\n")
	names := map[string]string{
		"\x1b[A": "UP", "\x1b[B": "DOWN", "\x1b[D": "LEFT", "\x1b[C": "RIGHT",
		"\r": "ENTER", "\x7f": "BACKSPACE", "\x1b": "ESC", "\x04": "EOT",
		"\x1b[3~": "DELETE", "\x1b[H": "HOME", "\x1b[F": "END", "\t": "TAB",
	}
	buf := make([]byte, 16)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return 1
		}
		in := string(buf[:n])
		if in == "q" {
			return 0
		}
		fmt.Printf("key=%s\r\n", names[in])
	}
}

func fixtureEnv([]string) int {
	rows, cols, _ := ptytest.WindowSize(os.Stdout.Fd())
	fmt.Printf("size=%dx%d\n", rows, cols)
	fmt.Printf("TERM=%s\nNO_COLOR=%s\nLC_ALL=%s\n", os.Getenv("TERM"), os.Getenv("NO_COLOR"), os.Getenv("LC_ALL"))
	fmt.Printf("HOME=%s\n", os.Getenv("HOME"))
	return 0
}

func fixtureWinch([]string) int {
	ch := make(chan os.Signal, 4)
	signal.Notify(ch, syscall.SIGWINCH)
	rows, cols, _ := ptytest.WindowSize(os.Stdout.Fd())
	fmt.Printf("size=%dx%d\n", rows, cols)
	<-ch
	rows, cols, _ = ptytest.WindowSize(os.Stdout.Fd())
	fmt.Printf("size=%dx%d\n", rows, cols)
	return 0
}

func fixtureCtrlC([]string) int {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	fmt.Println("ready")
	<-ch
	fmt.Println("interrupted")
	return 72
}

func fixtureBackCancel([]string) int {
	restore, err := ptytest.MakeRaw(os.Stdin.Fd())
	if err != nil {
		return 1
	}
	defer restore()
	step := 1
	buf := make([]byte, 16)
	for {
		fmt.Printf("step %d> ", step)
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return 1
		}
		switch string(buf[:n]) {
		case "\r":
			step++
		case "\x1b[D":
			if step > 1 {
				step--
			}
		}
		fmt.Print("\r\n")
		if step > 2 {
			fmt.Print("done\r\n")
			return 0
		}
	}
}

func fixtureHang([]string) int {
	signal.Ignore(syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	fmt.Println("up")
	block()
	return 0
}

func fixtureSpawn([]string) int {
	signal.Ignore(syscall.SIGHUP)
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "PTYTEST_FUNC=hang")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return 1
	}
	fmt.Printf("child=%d\n", cmd.Process.Pid)
	block()
	return 0
}

func fixtureHostileRaw([]string) int { fmt.Println(hostileText); return 0 }

func fixtureHostileSafe([]string) int { fmt.Println(terminal.Escape(hostileText)); return 0 }

func fixtureColored([]string) int { fmt.Print("\x1b[32mgreen\x1b[0m\n"); return 0 }

func fixtureRedraw([]string) int {
	fmt.Print("progress   0%\rprogress  50%\rprogress 100%\n")
	fmt.Print("junk\x1b[2K\rfinal\n")
	return 0
}

// block waits forever without tripping the runtime's deadlock detector.
func block() { time.Sleep(time.Hour) }

// fixtureFlood writes args[0] bytes of 80-column lines, then END.
func fixtureFlood(args []string) int {
	n, _ := strconv.Atoi(args[0])
	w := bufio.NewWriterSize(os.Stdout, 1<<16)
	line := strings.Repeat("x", 78) + "\n"
	for written := 0; written < n; written += len(line) {
		w.WriteString(line)
	}
	w.WriteString("END\n")
	w.Flush()
	return 0
}

// fixtureEmit writes the hex-decoded bytes of args[0] verbatim, then exits.
func fixtureEmit(args []string) int {
	b, err := hex.DecodeString(args[0])
	if err != nil {
		return 2
	}
	os.Stdout.Write(b)
	return 0
}

func fixtureEnvDump([]string) int {
	env := os.Environ()
	sort.Strings(env)
	for _, e := range env {
		fmt.Printf("ENV:%s\n", e)
	}
	return 0
}

func fixturePwd([]string) int {
	wd, _ := os.Getwd()
	fmt.Printf("PWD=%s\n", wd)
	return 0
}

func fixtureSelfKill([]string) int {
	fmt.Println("dying")
	syscall.Kill(os.Getpid(), syscall.SIGKILL)
	block()
	return 0
}

func fixtureQuiet([]string) int { fmt.Println("bye"); return 0 }

// fixtureLate prints "first", then hostile bytes only after reading a line,
// so a test that asserts before the process ends sees an incomplete transcript.
func fixtureLate([]string) int {
	fmt.Println("first")
	bufio.NewReader(os.Stdin).ReadString('\n')
	fmt.Print("\x1b[2Jlate\n")
	return 0
}

func fixtureWinchWide([]string) int {
	ch := make(chan os.Signal, 4)
	signal.Notify(ch, syscall.SIGWINCH)
	fmt.Println("ready")
	<-ch
	fmt.Println(strings.Repeat("w", 100))
	return 0
}

// fixtureFloodSeq writes the hex-decoded args[0] (an unterminated escape
// sequence), args[1] bytes of filler, then END.
func fixtureFloodSeq(args []string) int {
	b, _ := hex.DecodeString(args[0])
	n, _ := strconv.Atoi(args[1])
	w := bufio.NewWriterSize(os.Stdout, 1<<16)
	w.Write(b)
	filler := "x"
	if len(b) > 1 && b[1] == '[' {
		filler = "1" // digits keep a CSI unterminated
	}
	chunk := strings.Repeat(filler, 1024)
	for written := 0; written < n; written += len(chunk) {
		w.WriteString(chunk)
	}
	end := "\x07" // terminate the sequence so END is visible text
	if len(b) > 1 && b[1] == '[' {
		end = "m"
	}
	w.WriteString(end + "\nEND\n")
	w.Flush()
	return 0
}

// fixtureSplit writes a prefix and the start of a needle, waits for a line,
// then writes the rest of the needle.
func fixtureSplit([]string) int {
	restore, err := ptytest.MakeRaw(os.Stdin.Fd()) // no echo between the flushes
	if err != nil {
		return 1
	}
	defer restore()
	fmt.Print("xxxxxxxxABC")
	os.Stdin.Read(make([]byte, 1))
	fmt.Print("D\r\n")
	return 0
}

// fixturePartialOSC starts an operating system command and never ends it.
func fixturePartialOSC([]string) int {
	fmt.Print("\x1b]0;" + strings.Repeat("x", 1000))
	block()
	return 0
}
