//go:build unix

// SPDX-License-Identifier: EUPL-1.2
//
package localdocker

import (
	"encoding/json"
	"golang.org/x/sys/unix"
)

// At most 256 admitted effects each have an intent and observation/uncertainty
// row. State-only transitions are additional rows, not hidden quota losses.
const stageMaxJournalRecords = 2*256 + 16 + 1

func decodeStageJournal(data []byte) ([]stageRecord, error) {
	var rows []stageRecord
	if decodeStageClosed(data, &rows) != nil || len(rows) == 0 || len(rows) > stageMaxJournalRecords {
		return nil, stageConflict("journal integrity: malformed")
	}
	previous := ""
	for i, row := range rows {
		if row.Version != 1 || row.Sequence != i+1 || row.PreviousHash != previous || row.Hash != recordHash(row) || !lowerHex32(row.OperationID) || i > 0 && (row.OperationID != rows[0].OperationID || row.Fingerprint != rows[0].Fingerprint) {
			return nil, stageConflict("journal integrity: hash chain")
		}
		previous = row.Hash
	}
	return rows, nil
}
func (s *stageStore) archiveComplete(rows []stageRecord) error {
	op := rows[0].OperationID
	if _, e := s.loadReceipt(op); e != nil {
		return stageConflict("completed receipt requires owner reconciliation")
	}
	b, e := json.Marshal(rows)
	if e != nil {
		return e
	}
	if e = s.writeArtifact(op, "journal.json", append(b, '\n'), true); e != nil {
		return e
	}
	if e = unix.Unlinkat(int(s.dir.Fd()), "journal.json", 0); e != nil {
		return stageConflict("completed journal archive uncertain")
	}
	if e = s.dir.Sync(); e != nil {
		return stageConflict("completed journal archive durability uncertain")
	}
	return nil
}
func (s *stageStore) checkCompletedArchive(op string) error {
	b, e := s.readArtifact(op, "journal.json", 4<<20)
	if e != nil {
		return e
	}
	rows, e := decodeStageJournal(b)
	if e != nil {
		return e
	}
	last := rows[len(rows)-1]
	if last.OperationID != op || last.State != "complete" || last.Observation != "observed" {
		return stageConflict("retained attempt is incomplete")
	}
	b, e = s.readArtifact(op, "receipt.json", stageMaxArtifactBytes)
	if e != nil {
		return e
	}
	var receipt stageReceipt
	if decodeStageClosed(b, &receipt) != nil || receipt.Version != 1 || receipt.OperationID != op || receipt.Sequence != last.Sequence || receipt.PreviousHash != last.PreviousHash || receipt.State != "complete" || receipt.UnverifiedReason != nil {
		return stageConflict("completed receipt binding changed")
	}
	return nil
}
