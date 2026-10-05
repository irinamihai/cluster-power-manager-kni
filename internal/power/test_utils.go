package power

// Values host detection reports under PinTestHostIdentity. The sysfs fixtures
// the tests build are x86-shaped: they write topology/die_id, not cluster_id,
// so detection has to report x86_64 for the tests to pass on an aarch64 host.
const (
	testArchitecture = "x86_64"
	testVendorID     = "GenuineIntel"
)

// PinTestHostIdentity makes host detection independent of the machine the tests
// run on, pinning both the architecture and the /proc/cpuinfo vendor lookup.
// Returns a function that restores the previous values.
func PinTestHostIdentity() func() {
	originalArchitecture := getHostArchitecture
	originalReadCPUInfoField := readCPUInfoField

	getHostArchitecture = func() (string, error) { return testArchitecture, nil }
	readCPUInfoField = func(key string) (string, bool, error) {
		if key == "vendor_id" {
			return testVendorID, true, nil
		}
		// not found, not an error, as on a real host missing the key
		return "", false, nil
	}

	return func() {
		getHostArchitecture = originalArchitecture
		readCPUInfoField = originalReadCPUInfoField
	}
}
