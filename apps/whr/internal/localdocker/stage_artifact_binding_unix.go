// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
)

func promotionBytesDigest(data []byte) string {
	h := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(h[:])
}
func readPromotionArtifact(store *stageStore, op, name, expected string, dst any) error {
	if len(expected) != 71 || !strings.HasPrefix(expected, "sha256:") || !stageOID(expected[7:], "sha256") {
		return stageConflict("promotion private artifact binding invalid: " + name)
	}
	data, err := store.readArtifact(op, name, stageMaxArtifactBytes)
	if err != nil {
		return err
	}
	if promotionBytesDigest(data) != expected {
		return stageConflict("promotion private artifact digest changed: " + name)
	}
	check := json.NewDecoder(bytes.NewReader(data))
	if uniqueDockerValue(check, 0) != nil {
		return stageConflict("promotion private artifact fields invalid: " + name)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	dec.DisallowUnknownFields()
	if dec.Decode(dst) != nil || dec.Decode(new(any)) != io.EOF {
		return stageConflict("promotion private artifact fields invalid: " + name)
	}
	canonical, err := stageArtifactBytes(dst)
	if err != nil || !bytes.Equal(data, canonical) {
		return stageConflict("promotion private artifact is not canonical or has missing fields: " + name)
	}
	return nil
}
