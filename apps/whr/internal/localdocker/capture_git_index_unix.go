// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
)

func stageSharedIndex(data []byte, format string) (string, error) {
	hashBytes := 20
	if format == "sha256" {
		hashBytes = 32
	}
	if len(data) < 12+hashBytes || string(data[:4]) != "DIRC" {
		return "", stageConflict("Git index header unsupported")
	}
	version := binary.BigEndian.Uint32(data[4:8])
	count := binary.BigEndian.Uint32(data[8:12])
	if version < 2 || version > 4 || count > stageMaxEntries {
		return "", stageConflict("Git index version or entry bound unsupported")
	}
	pos := 12
	for i := uint32(0); i < count; i++ {
		start := pos
		fixed := 40 + hashBytes + 2
		if pos+fixed > len(data)-hashBytes {
			return "", stageConflict("Git index entry truncated")
		}
		flags := binary.BigEndian.Uint16(data[pos+fixed-2 : pos+fixed])
		pos += fixed
		if flags&0x4000 != 0 {
			pos += 2
		}
		if version == 4 {
			for n := 0; ; n++ {
				if n > 9 || pos >= len(data)-hashBytes {
					return "", stageConflict("Git index path compression malformed")
				}
				v := data[pos]
				pos++
				if v&0x80 == 0 {
					break
				}
			}
		}
		if pos >= len(data)-hashBytes {
			return "", stageConflict("Git index pathname truncated")
		}
		end := bytes.IndexByte(data[pos:len(data)-hashBytes], 0)
		if end < 0 || end > 4096 {
			return "", stageConflict("Git index pathname bound")
		}
		pos += end + 1
		if version != 4 {
			pos = start + ((pos-start+7)/8)*8
		}
	}
	for pos < len(data)-hashBytes {
		if pos+8 > len(data)-hashBytes {
			return "", stageConflict("Git index extension truncated")
		}
		name := string(data[pos : pos+4])
		size := int(binary.BigEndian.Uint32(data[pos+4 : pos+8]))
		pos += 8
		if size < 0 || size > len(data)-hashBytes-pos {
			return "", stageConflict("Git index extension size invalid")
		}
		if name == "link" {
			if size < hashBytes {
				return "", stageConflict("Git split-index link truncated")
			}
			sum := data[pos : pos+hashBytes]
			if bytes.Equal(sum, make([]byte, hashBytes)) {
				return "", nil
			}
			return "sharedindex." + hex.EncodeToString(sum), nil
		}
		pos += size
	}
	return "", nil
}
func prepareStageIndex(ctx context.Context, home, reader string, s stageGitSnapshot) ([]byte, []byte, error) {
	raw, err := stageAdminFile(s.GitDir, "index", stageMaxWorkingBytes)
	if os.IsNotExist(err) {
		raw = nil
		err = nil
	}
	if err != nil {
		return nil, nil, err
	}
	index := filepath.Join(reader, "index")
	if len(raw) > 0 {
		shared, err := stageSharedIndex(raw, s.ObjectFormat)
		if err != nil {
			return nil, nil, err
		}
		if err = os.WriteFile(index, raw, 0600); err != nil {
			return nil, nil, err
		}
		if shared != "" {
			data, err := stageAdminFile(s.GitDir, shared, stageMaxWorkingBytes)
			if err != nil {
				return nil, nil, stageConflict("Git split-index dependency unavailable")
			}
			if err = os.WriteFile(filepath.Join(reader, shared), data, 0600); err != nil {
				return nil, nil, err
			}
			if _, err = stageGitRead(ctx, home, reader, s.ObjectDir, index, nil, "update-index", "--no-split-index"); err != nil {
				return nil, nil, stageConflict("Git split-index normalization failed")
			}
			raw, err = os.ReadFile(index)
			if err != nil {
				return nil, nil, err
			}
		}
	}
	entries, err := stageGitReadLimit(ctx, home, reader, s.ObjectDir, index, nil, stageMaxEntries*(4096+128), "ls-files", "--stage", "-z")
	return entries, raw, err
}
