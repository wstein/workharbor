//go:build unix

// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// The approved entry/path bounds determine manifest size; the Docker metadata
// 4-MiB output budget is not a smaller quota for private source manifests.
const stageMaxArtifactBytes = stageMaxEntries * (4096 + 512)

func (s *stageStore) operationDirectory(op string) (*os.File, error) {
	if !lowerHex32(op) {
		return nil, stageConflict("receipt operation identity invalid")
	}
	fd, e := unix.Openat(int(s.dir.Fd()), op, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if e != nil {
		return nil, stageConflict("receipt directory unavailable")
	}
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || int(st.Uid) != os.Getuid() || st.Mode&0777 != 0700 {
		unix.Close(fd)
		return nil, stageConflict("receipt directory ownership or mode")
	}
	return os.NewFile(uintptr(fd), op), nil
}
func (s *stageStore) operationPath(op string) string { return filepath.Join(s.path, op) }
func (s *stageStore) writeArtifact(op, name string, data []byte, exclusive bool) error {
	if name == "" || filepath.Base(name) != name || strings.HasPrefix(name, ".") || len(data) > stageMaxArtifactBytes {
		return stageConflict("private artifact name or quota")
	}
	dir, e := s.operationDirectory(op)
	if e != nil {
		return e
	}
	defer dir.Close()
	if !exclusive {
		fd, err := unix.Openat(int(dir.Fd()), name, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return stageConflict("private artifact replacement unavailable")
		}
		var st unix.Stat_t
		err = unix.Fstat(fd, &st)
		unix.Close(fd)
		if err != nil || int(st.Uid) != os.Getuid() || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&0777 != 0600 || st.Nlink != 1 {
			return stageConflict("private artifact replacement identity")
		}
	}
	// An interrupted temporary publication stays visible and blocks another
	// publication; callers never erase an uncertain artifact to retry effects.
	temp := "." + name + ".next"
	fd, e := unix.Openat(int(dir.Fd()), temp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if e != nil {
		return stageConflict("private artifact publication retained or unavailable")
	}
	f := os.NewFile(uintptr(fd), temp)
	_, e = f.Write(data)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return stageConflict("private artifact publication failed")
	}
	if exclusive {
		e = unix.Linkat(int(dir.Fd()), temp, int(dir.Fd()), name, 0)
	} else {
		e = unix.Renameat(int(dir.Fd()), temp, int(dir.Fd()), name)
	}
	if e != nil {
		return stageConflict("private artifact publication failed")
	}
	if exclusive {
		if e = unix.Unlinkat(int(dir.Fd()), temp, 0); e != nil {
			return stageConflict("private artifact publication uncertain")
		}
	}
	if e = dir.Sync(); e != nil {
		return stageConflict("private artifact durability uncertain")
	}
	return nil
}
func (s *stageStore) readArtifact(op, name string, limit int64) ([]byte, error) {
	if filepath.Base(name) != name || strings.HasPrefix(name, ".") {
		return nil, stageConflict("private artifact name invalid")
	}
	dir, e := s.operationDirectory(op)
	if e != nil {
		return nil, e
	}
	defer dir.Close()
	fd, e := unix.Openat(int(dir.Fd()), name, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if e != nil {
		return nil, stageConflict("private artifact unavailable")
	}
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || int(st.Uid) != os.Getuid() || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&0777 != 0600 || st.Nlink != 1 || st.Size > limit {
		return nil, stageConflict("private artifact ownership, mode or quota")
	}
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil || int64(len(b)) > limit {
		return nil, stageConflict("private artifact read quota")
	}
	return b, nil
}
func (s *stageStore) saveReceipt(receipt *stageReceipt, exclusive bool) error {
	rows, e := s.Records()
	if e != nil {
		return e
	}
	if len(rows) == 0 || receipt.OperationID != rows[0].OperationID {
		return stageConflict("receipt lacks bound durable intent")
	}
	last := rows[len(rows)-1]
	receipt.Version = 1
	receipt.State = last.State
	// Bind the replacement receipt with a new append-only journal row first.
	// Interrupted publication produces a mismatch requiring reconciliation.
	if len(rows) >= stageMaxJournalRecords {
		return stageConflict("journal record quota")
	}
	receipt.Sequence = last.Sequence + 1
	receipt.PreviousHash = last.Hash
	b, e := json.Marshal(receipt)
	if e != nil {
		return stageConflict("receipt target encoding invalid")
	}
	var check stageReceipt
	if e = decodeStageClosed(b, &check); e != nil {
		return stageConflict("receipt closed fields invalid")
	}
	rows = append(rows, stageRecord{Version: 1, OperationID: last.OperationID, Sequence: receipt.Sequence, PreviousHash: last.Hash, State: last.State, Fingerprint: last.Fingerprint, Action: last.Action, Observation: last.Observation, ReceiptDigest: stageReceiptDigest(*receipt)})
	if e = s.write(rows, false); e != nil {
		return e
	}
	return s.writeArtifact(receipt.OperationID, "receipt.json", append(b, '\n'), exclusive)
}
func (s *stageStore) loadReceipt(op string) (stageReceipt, error) {
	var receipt stageReceipt
	b, e := s.readArtifact(op, "receipt.json", stageMaxArtifactBytes)
	if e != nil {
		return receipt, e
	}
	if e = decodeStageClosed(b, &receipt); e != nil {
		return receipt, stageConflict("receipt closed fields invalid")
	}
	rows, e := s.Records()
	if e != nil || len(rows) == 0 {
		return receipt, stageConflict("receipt journal unavailable")
	}
	last := rows[len(rows)-1]
	if receipt.Version != 1 || receipt.OperationID != op || receipt.OperationID != last.OperationID || receipt.Sequence != last.Sequence || receipt.PreviousHash != last.PreviousHash || receipt.State != last.State {
		return receipt, stageConflict("receipt journal binding changed")
	}
	if last.ReceiptDigest == "" || last.ReceiptDigest != stageReceiptDigest(receipt) {
		return receipt, stageConflict("receipt hash-chain binding changed")
	}
	if receipt.State == "staged_unverified" {
		if receipt.UnverifiedReason == nil {
			return receipt, stageConflict("receipt unverified reason missing")
		}
		switch *receipt.UnverifiedReason {
		case "missing_smoke", "failed_smoke", "failed_health", "stale_candidate_inputs":
		default:
			return receipt, stageConflict("receipt unverified reason invalid")
		}
	} else if receipt.UnverifiedReason != nil {
		return receipt, stageConflict("receipt unexpected unverified reason")
	}
	return receipt, nil
}

func stageReceiptDigest(receipt stageReceipt) string {
	receipt.Sequence = 0
	receipt.PreviousHash = ""
	return stageDigest(receipt)
}
