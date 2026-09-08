package power

import (
	"fmt"
)

// TestGetFromLscpu should be used in tests instead of power.GetFromLscpu.
var TestGetFromLscpu = func(regex string) (string, error) {
	if regex == "^Architecture:" {
		return "x86_64", nil
	}
	if regex == "^Vendor ID:" {
		return "GenuineIntel", nil
	}
	return "", fmt.Errorf("unsupported regex")
}
