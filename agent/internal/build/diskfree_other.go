//go:build !linux

package build

// The agent runs on Linux. Other platforms only compile and run the tests, so
// they report enough space unless a test replaces the measurement.
func freeBytes(string) (uint64, error) {
	return 1 << 40, nil
}
