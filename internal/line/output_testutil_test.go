package line

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"testing"
)

// Shared output-test helpers. They live in an untagged file so the read-side
// output tests (find-file, stats, …) can use them in every build, including the
// open build, while the media/download tests that also use them are build-tagged.

func outKey(b byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32)) }

func outJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func outStatuses[T any](rows []T, f func(T) string) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = f(r)
	}
	return out
}
