package scaling

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cluster-power-manager/cluster-power-manager/internal/power"
)

// setupScalingTestFiles creates a mock filesystem for scaling tests
func setupScalingTestFiles(cores int, cpufiles map[string]string) (power.Host, func(), error) {
	path := "testing/cpus"

	// Required files for CPU frequency operations
	pStatesDrvFile := "cpufreq/scaling_driver"
	scalingGovFile := "cpufreq/scaling_governor"
	scalingSetSpeedFile := "cpufreq/scaling_setspeed"
	scalingCurFreqFile := "cpufreq/scaling_cur_freq"
	cpuMaxFreqFile := "cpufreq/cpuinfo_max_freq"
	cpuMinFreqFile := "cpufreq/cpuinfo_min_freq"
	scalingMaxFile := "cpufreq/scaling_max_freq"
	scalingMinFile := "cpufreq/scaling_min_freq"
	availGovFile := "cpufreq/scaling_available_governors"

	// Create directories and files for test CPUs
	for i := 0; i < cores; i++ {
		cpuName := "cpu" + fmt.Sprint(i)
		cpudir := filepath.Join(path, cpuName)
		os.MkdirAll(filepath.Join(cpudir, "cpufreq"), os.ModePerm)
		os.MkdirAll(filepath.Join(cpudir, "topology"), os.ModePerm)

		// Create required files with default or custom values
		fileMap := map[string]string{
			"driver":              getFileValue(cpufiles, "driver", "intel_pstate"),
			"governor":            getFileValue(cpufiles, "governor", "userspace"),
			"setspeed":            getFileValue(cpufiles, "setspeed", "2000000"),
			"current":             getFileValue(cpufiles, "current", "2000000"),
			"max":                 getFileValue(cpufiles, "max", "3700000"),
			"min":                 getFileValue(cpufiles, "min", "400000"),
			"available_governors": getFileValue(cpufiles, "available_governors", "performance powersave schedutil userspace"),
		}

		for prop, value := range fileMap {
			switch prop {
			case "driver":
				os.WriteFile(filepath.Join(cpudir, pStatesDrvFile), []byte(value+"\n"), 0o644)
			case "governor":
				os.WriteFile(filepath.Join(cpudir, scalingGovFile), []byte(value+"\n"), 0o644)
			case "setspeed":
				os.WriteFile(filepath.Join(cpudir, scalingSetSpeedFile), []byte(value+"\n"), 0o644)
			case "current":
				os.WriteFile(filepath.Join(cpudir, scalingCurFreqFile), []byte(value+"\n"), 0o644)
			case "max":
				os.WriteFile(filepath.Join(cpudir, scalingMaxFile), []byte(value+"\n"), 0o644)
				os.WriteFile(filepath.Join(cpudir, cpuMaxFreqFile), []byte(value+"\n"), 0o644)
			case "min":
				os.WriteFile(filepath.Join(cpudir, scalingMinFile), []byte(value+"\n"), 0o644)
				os.WriteFile(filepath.Join(cpudir, cpuMinFreqFile), []byte(value+"\n"), 0o644)
			case "available_governors":
				os.WriteFile(filepath.Join(cpudir, availGovFile), []byte(value+"\n"), 0o644)
			}
		}

		// Minimal topology info
		os.WriteFile(filepath.Join(cpudir, "topology", "physical_package_id"), []byte("0\n"), 0o664)
		os.WriteFile(filepath.Join(cpudir, "topology", "die_id"), []byte("0\n"), 0o664)
		os.WriteFile(filepath.Join(cpudir, "topology", "core_id"), []byte(fmt.Sprint(i)+"\n"), 0o664)
	}

	// Create power library instance with custom CPU path
	originalGetFromLscpu := power.GetFromLscpu
	power.GetFromLscpu = power.TestGetFromLscpu
	host, err := power.CreateInstanceWithConf("test-node", power.LibConfig{
		CPUPath:    "testing/cpus",
		ModulePath: "testing/proc.modules",
		Cores:      uint(cores),
	})
	if host == nil {
		return nil, nil, err
	}

	return host, func() {
		os.RemoveAll(strings.Split(path, "/")[0])
		power.GetFromLscpu = originalGetFromLscpu
	}, nil
}

// getFileValue returns custom value from cpufiles map or default value
func getFileValue(cpufiles map[string]string, key, defaultValue string) string {
	if value, exists := cpufiles[key]; exists {
		return value
	}
	return defaultValue
}
