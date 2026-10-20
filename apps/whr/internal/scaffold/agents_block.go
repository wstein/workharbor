// SPDX-License-Identifier: EUPL-1.2
package scaffold

import (
	"bytes"
	_ "embed"
	"fmt"
	"regexp"
	"strconv"
	"unicode"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

const agentsBlockVersion = 1
const agentsBlockMaxBytes = 64 * 1024

//go:embed agents_workflow.txt
var agentsBlockBody string

var agentsBegin = regexp.MustCompile(`^<!-- whr:agents-block begin v=([0-9]+) sha256=([0-9a-f]{64}) -->\n$`)
var agentsEnd = []byte("<!-- whr:agents-block end -->\n")

type agentsBlockPlan struct {
	action, warning string
	data            []byte
}

type agentsBlock struct {
	start, bodyStart, bodyEnd, end int
	version                        uint64
	hash                           string
}

func markerLike(line []byte) bool {
	line = bytes.TrimSuffix(line, []byte("\r"))
	line = bytes.TrimLeftFunc(line, unicode.IsSpace)
	line = bytes.TrimPrefix(line, []byte("\xef\xbb\xbf"))
	line = bytes.TrimLeftFunc(line, unicode.IsSpace)
	return bytes.HasPrefix(line, []byte("<!-- whr:agents-block"))
}

// parseAgentsBlock accepts only exact LF markers. Everything else resembling
// a marker is malformed, even inside a fence or outside the managed block.
func parseAgentsBlock(data []byte) (agentsBlock, bool) {
	var block agentsBlock
	seen, open := false, false
	for offset := 0; offset < len(data); {
		n := bytes.IndexByte(data[offset:], '\n')
		end := len(data)
		if n >= 0 {
			end = offset + n + 1
		}
		line := data[offset:end]
		if match := agentsBegin.FindSubmatch(line); match != nil {
			if seen {
				return block, false
			}
			version, err := strconv.ParseUint(string(match[1]), 10, 64)
			if err != nil {
				return block, false
			}
			block = agentsBlock{start: offset, bodyStart: end, version: version, hash: string(match[2])}
			seen, open = true, true
		} else if bytes.Equal(line, agentsEnd) {
			if !open {
				return block, false
			}
			block.bodyEnd, block.end = offset, end
			open = false
		} else if markerLike(bytes.TrimSuffix(line, []byte("\n"))) {
			return block, false
		}
		offset = end
	}
	return block, !open
}

func renderAgentsBlock() []byte {
	return []byte(fmt.Sprintf("<!-- whr:agents-block begin v=%d sha256=%s -->\n%s%s", agentsBlockVersion, digest([]byte(agentsBlockBody)), agentsBlockBody, agentsEnd))
}

// planAgentsBlock implements the single FR-064 precedence and edit predicate.
// Filesystem kind, descriptor size and consent are handled by the caller.
func planAgentsBlock(data []byte, edit bool) (agentsBlockPlan, error) {
	return planAgentsBlockSized(data, int64(len(data)), edit)
}

func planAgentsBlockSized(data []byte, size int64, edit bool) (agentsBlockPlan, error) {
	class := "missing"
	var block agentsBlock
	if size > agentsBlockMaxBytes || len(data) > agentsBlockMaxBytes {
		class = "too_large"
	} else {
		var valid bool
		block, valid = parseAgentsBlock(data)
		switch {
		case !valid:
			class = "malformed"
		case block.end == 0:
			class = "missing"
		case digest(data[block.bodyStart:block.bodyEnd]) != block.hash:
			class = "modified"
		case block.version == agentsBlockVersion && bytes.Equal(data[block.bodyStart:block.bodyEnd], []byte(agentsBlockBody)):
			class = "current"
		default:
			class = "different_template"
		}
	}
	if class == "current" {
		return agentsBlockPlan{action: "skip_existing"}, nil
	}
	var result []byte
	action := "replace_block"
	if class == "modified" || class == "different_template" || class == "missing" {
		if class == "missing" {
			action = "append_block"
			result = append(result, data...)
			if len(result) > 0 && result[len(result)-1] != '\n' {
				result = append(result, '\n')
			}
			result = append(result, '\n')
			result = append(result, renderAgentsBlock()...)
		} else {
			result = append(result, data[:block.start]...)
			result = append(result, renderAgentsBlock()...)
			result = append(result, data[block.end:]...)
		}
		if len(result) > agentsBlockMaxBytes {
			class = "too_large"
		}
	}
	if edit && (class == "too_large" || class == "malformed") {
		return agentsBlockPlan{}, exitcode.Errorf(exitcode.PathConflict, "AGENTS.md agent workflow block is %s; no files were written", class)
	}
	if !edit {
		return agentsBlockPlan{action: "left_unchanged", warning: "agents_block." + class}, nil
	}
	return agentsBlockPlan{action: action, data: result}, nil
}
