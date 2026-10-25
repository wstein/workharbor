// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
)

// stageScanIndex validates the entry bound before reading the body. Extension
// payloads are skipped with fixed storage rather than allocated at their size.
func stageScanIndex(input io.Reader, size int64, format string) (string, error) {
	hashBytes := 20
	if format == "sha256" {
		hashBytes = 32
	}
	header := make([]byte, 12)
	if size < int64(12+hashBytes) {
		return "", stageConflict("Git index header unsupported")
	}
	if _, err := io.ReadFull(input, header); err != nil || string(header[:4]) != "DIRC" {
		return "", stageConflict("Git index header unsupported")
	}
	version := binary.BigEndian.Uint32(header[4:8])
	count := binary.BigEndian.Uint32(header[8:12])
	if version < 2 || version > 4 || count > stageMaxEntries {
		return "", stageConflict("Git index version or entry bound unsupported")
	}
	payload := &io.LimitedReader{R: input, N: size - int64(12+hashBytes)}
	reader := bufio.NewReader(payload)
	remaining := payload.N
	consumed := int64(0)
	discard := func(n int64) error {
		if n < 0 || n > remaining-consumed {
			return io.ErrUnexpectedEOF
		}
		_, err := io.CopyN(io.Discard, reader, n)
		consumed += n
		return err
	}
	for i := uint32(0); i < count; i++ {
		start := consumed
		fixed := make([]byte, 40+hashBytes+2)
		if int64(len(fixed)) > remaining-consumed {
			return "", stageConflict("Git index entry truncated")
		}
		if _, err := io.ReadFull(reader, fixed); err != nil {
			return "", stageConflict("Git index entry truncated")
		}
		consumed += int64(len(fixed))
		flags := binary.BigEndian.Uint16(fixed[len(fixed)-2:])
		if flags&0x4000 != 0 {
			if discard(2) != nil {
				return "", stageConflict("Git index entry truncated")
			}
		}
		if version == 4 {
			for n := 0; ; n++ {
				if n > 9 || consumed >= remaining {
					return "", stageConflict("Git index path compression malformed")
				}
				v, err := reader.ReadByte()
				if err != nil {
					return "", stageConflict("Git index path compression malformed")
				}
				consumed++
				if v&0x80 == 0 {
					break
				}
			}
		}
		for n := 0; ; n++ {
			if consumed >= remaining {
				return "", stageConflict("Git index pathname truncated")
			}
			v, err := reader.ReadByte()
			if err != nil {
				return "", stageConflict("Git index pathname truncated")
			}
			consumed++
			if n > 4096 {
				return "", stageConflict("Git index pathname bound")
			}
			if v == 0 {
				break
			}
		}
		if version != 4 {
			padding := (8 - (consumed-start)%8) % 8
			if discard(padding) != nil {
				return "", stageConflict("Git index entry truncated")
			}
		}
	}
	shared := ""
	for consumed < remaining {
		var extension [8]byte
		if remaining-consumed < 8 {
			return "", stageConflict("Git index extension truncated")
		}
		if _, err := io.ReadFull(reader, extension[:]); err != nil {
			return "", stageConflict("Git index extension truncated")
		}
		consumed += 8
		length := int64(binary.BigEndian.Uint32(extension[4:]))
		if length > remaining-consumed {
			return "", stageConflict("Git index extension size invalid")
		}
		if string(extension[:4]) == "link" {
			if length < int64(hashBytes) {
				return "", stageConflict("Git split-index link truncated")
			}
			sum := make([]byte, hashBytes)
			if _, err := io.ReadFull(reader, sum); err != nil {
				return "", stageConflict("Git split-index link truncated")
			}
			consumed += int64(hashBytes)
			length -= int64(hashBytes)
			if !bytes.Equal(sum, make([]byte, hashBytes)) {
				shared = "sharedindex." + hex.EncodeToString(sum)
			}
		}
		if discard(length) != nil {
			return "", stageConflict("Git index extension size invalid")
		}
	}
	if _, err := io.CopyN(io.Discard, input, int64(hashBytes)); err != nil {
		return "", stageConflict("Git index checksum truncated")
	}
	var extra [1]byte
	if n, err := input.Read(extra[:]); n != 0 || err != io.EOF {
		return "", stageConflict("Git index changed while reading")
	}
	return shared, nil
}

// stageReadIndex optionally copies the same validated bytes into private storage.
func stageReadIndex(ctx context.Context, root, name, format, destination string) (string, string, error) {
	file, err := stageAdminOpen(root, name, stageMaxWorkingBytes)
	if err != nil {
		return "", "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", "", stageConflict("Git index metadata unavailable")
	}
	hash := sha256.New()
	output := io.Writer(hash)
	if destination != "" {
		copied, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err != nil {
			return "", "", stageConflict("private Git index publication failed")
		}
		defer copied.Close()
		output = io.MultiWriter(hash, copied)
	}
	reader := io.TeeReader(stageContextReader{ctx: ctx, Reader: io.LimitReader(file, stageMaxWorkingBytes+1)}, output)
	shared, err := stageScanIndex(reader, info.Size(), format)
	if ctx.Err() != nil {
		return "", "", ctx.Err()
	}
	if err != nil {
		return "", "", err
	}
	return shared, "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func stageIndexBinding(ctx context.Context, s stageGitSnapshot) (string, error) {
	shared, digest, err := stageReadIndex(ctx, s.GitDir, "index", s.ObjectFormat, "")
	if os.IsNotExist(err) {
		empty := sha256.Sum256(nil)
		return "sha256:" + hex.EncodeToString(empty[:]), nil
	}
	if err != nil || shared == "" {
		return digest, err
	}
	nested, sharedDigest, err := stageReadIndex(ctx, s.GitDir, shared, s.ObjectFormat, "")
	if os.IsNotExist(err) {
		return "", stageConflict("Git split-index dependency unavailable")
	}
	if err != nil {
		return "", err
	}
	if nested != "" {
		return "", stageConflict("Git nested split-index unsupported")
	}
	sum := sha256.Sum256([]byte(digest + "\x00" + shared + "\x00" + sharedDigest))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func prepareStageIndex(ctx context.Context, home, reader string, s stageGitSnapshot) ([]byte, string, error) {
	index := filepath.Join(reader, "index")
	shared, _, err := stageReadIndex(ctx, s.GitDir, "index", s.ObjectFormat, index)
	rawPath := index
	if os.IsNotExist(err) {
		rawPath, err = "", nil
	}
	if err != nil {
		return nil, "", err
	}
	if shared != "" {
		nested, _, err := stageReadIndex(ctx, s.GitDir, shared, s.ObjectFormat, filepath.Join(reader, shared))
		if os.IsNotExist(err) {
			return nil, "", stageConflict("Git split-index dependency unavailable")
		}
		if err != nil {
			return nil, "", err
		}
		if nested != "" {
			return nil, "", stageConflict("Git nested split-index unsupported")
		}
		if _, err = stageGitRead(ctx, home, reader, s.ObjectDir, index, nil, "update-index", "--no-split-index"); err != nil {
			return nil, "", stageConflict("Git split-index normalization failed")
		}
		if _, _, err = stageReadIndex(ctx, reader, "index", s.ObjectFormat, ""); err != nil {
			return nil, "", err
		}
	}
	entries, err := stageGitReadLimit(ctx, home, reader, s.ObjectDir, index, nil, stageMaxEntries*(4096+128), "ls-files", "--stage", "-z")
	return entries, rawPath, err
}
