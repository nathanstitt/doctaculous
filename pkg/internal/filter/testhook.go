package filter

import "testing"

// SetMaxDecodedSizeForTest lowers the decoded-size ceiling for the duration of a
// test and restores it afterwards. It lives in the production package, rather
// than in an _test.go file, because the tests that need it are in pkg/pdf and an
// _test.go file is not importable from another package.
//
// It exists so the compression-bomb tests can exercise the real refusal path
// without the half-gigabyte allocation the production limit would otherwise
// require; see [maxDecodedSize] for the measurement.
//
// The limit is process-wide, so tests that call this must not run in parallel.
func SetMaxDecodedSizeForTest(t *testing.T, n int64) {
	t.Helper()
	prev := maxDecodedSize
	maxDecodedSize = n
	t.Cleanup(func() { maxDecodedSize = prev })
}
