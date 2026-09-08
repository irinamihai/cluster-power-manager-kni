package power

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func setupCPUScalingTests(cpufiles map[string]map[string]string) func() {
	origBasePath := basePath
	basePath = "testing/cpus"
	allCPUDefaultPStatesInfoCopy := allCPUDefaultPStatesInfo
	typeCopy := coreTypes
	// backup pointer to function that gets all CPUs
	// replace it with our controlled function
	origGetNumOfCpusFunc := getNumberOfCpus
	getNumberOfCpus = func() uint { return uint(len(cpufiles)) }

	// "initialise" P-States feature
	featureList[FrequencyScalingFeature].err = nil

	// Initialize allCPUDefaultPStatesInfo for all CPUs in the map
	numCpus := len(cpufiles)
	allCPUDefaultPStatesInfo = make([]pstatesImpl, numCpus)

	// Set up default P-states info for each CPU
	for cpuName, cpuDetails := range cpufiles {
		// Extract CPU ID from the name (e.g., "cpu0" -> 0, "cpu1" -> 1)
		cpuIDStr := cpuName[3:] // Remove "cpu" prefix
		cpuID, err := strconv.Atoi(cpuIDStr)
		if err != nil {
			continue // Skip invalid CPU names
		}

		// Initialize with defaults
		allCPUDefaultPStatesInfo[cpuID] = pstatesImpl{
			governor: defaultGovernor,
			epp:      defaultEpp,
		}

		// Set values from the CPU details
		if max, ok := cpuDetails["max"]; ok {
			if maxInt, err := strconv.Atoi(max); err == nil {
				allCPUDefaultPStatesInfo[cpuID].maxFreq = intstr.FromInt(maxInt)
			}
		}
		if min, ok := cpuDetails["min"]; ok {
			if minInt, err := strconv.Atoi(min); err == nil {
				allCPUDefaultPStatesInfo[cpuID].minFreq = intstr.FromInt(minInt)
			}
		}
		if governor, ok := cpuDetails["governor"]; ok {
			allCPUDefaultPStatesInfo[cpuID].governor = governor
		}
		if epp, ok := cpuDetails["epp"]; ok {
			allCPUDefaultPStatesInfo[cpuID].epp = epp
		}
	}
	for cpuName, cpuDetails := range cpufiles {
		cpudir := filepath.Join(basePath, cpuName)
		os.MkdirAll(filepath.Join(cpudir, "cpufreq"), os.ModePerm)
		os.MkdirAll(filepath.Join(cpudir, "topology"), os.ModePerm)
		for prop, value := range cpuDetails {
			switch prop {
			case "driver":
				os.WriteFile(filepath.Join(cpudir, pStatesDrvFile), []byte(value+"\n"), 0o664)
			case "max":
				os.WriteFile(filepath.Join(cpudir, scalingMaxFile), []byte(value+"\n"), 0o644)
				os.WriteFile(filepath.Join(cpudir, cpuMaxFreqFile), []byte(value+"\n"), 0o644)
			case "min":
				os.WriteFile(filepath.Join(cpudir, scalingMinFile), []byte(value+"\n"), 0o644)
				os.WriteFile(filepath.Join(cpudir, cpuMinFreqFile), []byte(value+"\n"), 0o644)
			case "package":
				os.WriteFile(filepath.Join(cpudir, packageIDFile), []byte(value+"\n"), 0o644)
			case "die":
				os.WriteFile(filepath.Join(cpudir, dieIDFile), []byte(value+"\n"), 0o644)
				os.WriteFile(filepath.Join(cpudir, coreIDFile), []byte(cpuName[3:]+"\n"), 0o644)
			case "epp":
				os.WriteFile(filepath.Join(cpudir, eppFile), []byte(value+"\n"), 0o644)
			case "governor":
				os.WriteFile(filepath.Join(cpudir, scalingGovFile), []byte(value+"\n"), 0o644)
			case "available_governors":
				os.WriteFile(filepath.Join(cpudir, availGovFile), []byte(value+"\n"), 0o644)
			}
		}
	}
	return func() {
		// wipe created cpus dir
		os.RemoveAll(strings.Split(basePath, "/")[0])
		// revert cpu /sys path
		basePath = origBasePath
		// revert get number of system cpus function
		getNumberOfCpus = origGetNumOfCpusFunc
		// revert scaling driver feature to un initialised state
		featureList[FrequencyScalingFeature].err = errUninitialized
		coreTypes = typeCopy
		// revert default pstates
		allCPUDefaultPStatesInfo = allCPUDefaultPStatesInfoCopy
	}
}

func TestIsScalingDriverSupported(t *testing.T) {
	assert.False(t, isScalingDriverSupported("something"))
	assert.True(t, isScalingDriverSupported("intel_pstate"))
	assert.True(t, isScalingDriverSupported("intel_cpufreq"))
	assert.True(t, isScalingDriverSupported("acpi-cpufreq"))
}
func TestPreChecksScalingDriver(t *testing.T) {
	var pStates featureStatus
	origpath := basePath
	basePath = ""
	pStates = initScalingDriver()

	assert.Equal(t, pStates.name, "Frequency-Scaling")
	assert.ErrorContains(t, pStates.err, "failed to determine driver")
	epp := initEpp()
	assert.Equal(t, epp.name, "Energy-Performance-Preference")
	assert.ErrorContains(t, epp.err, "EPP file cpufreq/energy_performance_preference does not exist")
	basePath = origpath
	teardown := setupCPUScalingTests(map[string]map[string]string{
		"cpu0": {
			"min":                 "111",
			"max":                 "999",
			"driver":              "intel_pstate",
			"available_governors": "performance",
			"epp":                 "performance",
		},
	})

	pStates = initScalingDriver()
	assert.Equal(t, "intel_pstate", pStates.driver)
	assert.NoError(t, pStates.err)
	epp = initEpp()
	assert.NoError(t, epp.err)

	teardown()
	defer setupCPUScalingTests(map[string]map[string]string{
		"cpu0": {
			"driver": "some_unsupported_driver",
		},
	})()

	pStates = initScalingDriver()
	assert.ErrorContains(t, pStates.err, "unsupported")
	assert.Equal(t, pStates.driver, "some_unsupported_driver")
	teardown()
	defer setupCPUScalingTests(map[string]map[string]string{
		"cpu0": {
			"driver":              "acpi-cpufreq",
			"available_governors": "powersave",
			"max":                 "3700",
			"min":                 "3200",
		},
	})()
	acpi := initScalingDriver()
	assert.Equal(t, "acpi-cpufreq", acpi.driver)
	assert.NoError(t, acpi.err)
}

func TestCoreImpl_updateFreqValues(t *testing.T) {
	var core *cpuImpl
	const (
		maxDefault   = 9990
		maxFreqToSet = 8888
		minFreqToSet = 1000
	)
	typeCopy := coreTypes
	coreTypes = CoreTypeList{&CPUFrequencySet{min: minFreqToSet, max: maxDefault}}
	defer func() { coreTypes = typeCopy }()

	core = &cpuImpl{}
	// p-states not supported
	assert.NoError(t, core.updateFrequencies())

	teardown := setupCPUScalingTests(map[string]map[string]string{
		"cpu0": {
			"max": fmt.Sprint(maxDefault),
			"min": fmt.Sprint(minFreqToSet),
		},
	})

	defer teardown()

	// set desired power profile
	host := new(hostMock)
	pool := new(poolMock)
	core = &cpuImpl{
		id:   0,
		pool: pool,
		core: &cpuCore{coreType: 0},
	}
	profile := &profileImpl{pstates: &pstatesImpl{maxFreq: intstr.FromInt(int(maxFreqToSet)), minFreq: intstr.FromInt(int(minFreqToSet))}, cstates: cstatesImpl{}}
	pool.On("GetPowerProfile").Return(profile)
	pool.On("getHost").Return(host)
	host.On("NumCoreTypes").Return(uint(1))

	assert.NoError(t, core.updateFrequencies())
	maxFreqContent, _ := os.ReadFile(filepath.Join(basePath, "cpu0", scalingMaxFile))
	maxFreqInt, _ := strconv.Atoi(string(maxFreqContent))
	assert.Equal(t, maxFreqToSet, maxFreqInt)
	pool.AssertNumberOfCalls(t, "GetPowerProfile", 1)

	// set default power profile
	pool = new(poolMock)
	core.pool = pool
	pool.On("GetPowerProfile").Return(nil)
	pool.On("getHost").Return(host)
	assert.NoError(t, core.updateFrequencies())
	maxFreqContent, _ = os.ReadFile(filepath.Join(basePath, "cpu0", scalingMaxFile))
	maxFreqInt, _ = strconv.Atoi(string(maxFreqContent))
	assert.Equal(t, maxDefault, maxFreqInt)
	pool.AssertNumberOfCalls(t, "GetPowerProfile", 1)

}

func TestCoreImpl_setPstatsValues(t *testing.T) {
	const (
		maxFreqToSet  = 8888
		minFreqToSet  = 1111
		governorToSet = "powersave"
		eppToSet      = "testEpp"
	)
	featureList[FrequencyScalingFeature].err = nil
	featureList[EPPFeature].err = nil
	typeCopy := coreTypes
	coreTypes = CoreTypeList{&CPUFrequencySet{min: 1000, max: 9000}}
	defer func() { coreTypes = typeCopy }()
	defer func() { featureList[EPPFeature].err = errUninitialized }()
	defer func() { featureList[FrequencyScalingFeature].err = errUninitialized }()

	poolmk := new(poolMock)
	host := new(hostMock)
	poolmk.On("getHost").Return(host)
	host.On("NumCoreTypes").Return(uint(1))
	core := &cpuImpl{
		id:   0,
		core: &cpuCore{id: 0, coreType: 0},
		pool: poolmk,
	}

	teardown := setupCPUScalingTests(map[string]map[string]string{
		"cpu0": {
			"governor": "performance",
			"max":      "9999",
			"min":      "999",
			"epp":      "balance-performance",
		},
	})
	defer teardown()

	pstatesConfig := &pstatesImpl{
		maxFreq:  intstr.FromInt(maxFreqToSet),
		minFreq:  intstr.FromInt(minFreqToSet),
		epp:      eppToSet,
		governor: governorToSet,
	}
	assert.NoError(t, core.setDriverValues(pstatesConfig))

	governorFileContent, _ := os.ReadFile(filepath.Join(basePath, "cpu0", scalingGovFile))
	assert.Equal(t, governorToSet, string(governorFileContent))

	eppFileContent, _ := os.ReadFile(filepath.Join(basePath, "cpu0", eppFile))
	assert.Equal(t, eppToSet, string(eppFileContent))

	maxFreqContent, _ := os.ReadFile(filepath.Join(basePath, "cpu0", scalingMaxFile))
	maxFreqInt, _ := strconv.Atoi(string(maxFreqContent))
	assert.Equal(t, maxFreqToSet, maxFreqInt)

	minFreqContent, _ := os.ReadFile(filepath.Join(basePath, "cpu0", scalingMaxFile))
	minFreqInt, _ := strconv.Atoi(string(minFreqContent))
	assert.Equal(t, maxFreqToSet, minFreqInt)

	// check for empty epp unset
	pstatesConfig.epp = ""
	assert.NoError(t, core.setDriverValues(pstatesConfig))
	eppFileContent, _ = os.ReadFile(filepath.Join(basePath, "cpu0", eppFile))
	assert.Equal(t, eppToSet, string(eppFileContent))
}

func TestCpuImpl_setDriverValues(t *testing.T) {
	const (
		cpuID          = 0
		maxFreqDefault = 3000
		minFreqDefault = 1000
		maxFreqToSet   = 2800
		minFreqToSet   = 1200
		governorToSet  = "performance"
		eppToSet       = "balance_power"
	)

	// Setup test environment
	featureList[FrequencyScalingFeature].err = nil
	featureList[EPPFeature].err = nil
	typeCopy := coreTypes
	coreTypes = CoreTypeList{&CPUFrequencySet{min: minFreqDefault, max: maxFreqDefault}}
	allCPUDefaultPStatesInfoCopy := allCPUDefaultPStatesInfo
	allCPUDefaultPStatesInfo = make([]pstatesImpl, 1)
	allCPUDefaultPStatesInfo[0] = pstatesImpl{
		maxFreq:  intstr.FromInt(maxFreqDefault),
		minFreq:  intstr.FromInt(minFreqDefault),
		governor: "powersave",
		epp:      "balance_performance",
	}

	defer func() {
		coreTypes = typeCopy
		allCPUDefaultPStatesInfo = allCPUDefaultPStatesInfoCopy
		featureList[FrequencyScalingFeature].err = errUninitialized
		featureList[EPPFeature].err = errUninitialized
	}()

	teardown := setupCPUScalingTests(map[string]map[string]string{
		"cpu0": {
			"governor": "powersave",
			"max":      fmt.Sprint(maxFreqDefault),
			"min":      fmt.Sprint(minFreqDefault),
			"epp":      "balance_performance",
		},
	})
	defer teardown()

	// Create CPU instance with mocked dependencies
	poolMock := new(poolMock)
	hostMock := new(hostMock)
	poolMock.On("getHost").Return(hostMock)
	hostMock.On("NumCoreTypes").Return(uint(1))

	cpu := &cpuImpl{
		id:   cpuID,
		core: &cpuCore{id: cpuID, coreType: 0},
		pool: poolMock,
	}

	tests := []struct {
		name          string
		pstates       PStates
		expectError   bool
		errorContains string
		validateFiles func(t *testing.T)
	}{
		{
			name: "successful setDriverValues with all fields",
			pstates: &pstatesImpl{
				maxFreq:  intstr.FromInt(maxFreqToSet),
				minFreq:  intstr.FromInt(minFreqToSet),
				governor: governorToSet,
				epp:      eppToSet,
			},
			expectError: false,
			validateFiles: func(t *testing.T) {
				// Verify governor file
				governorContent, err := os.ReadFile(filepath.Join(basePath, "cpu0", scalingGovFile))
				assert.NoError(t, err)
				assert.Equal(t, governorToSet, string(governorContent))

				// Verify EPP file
				eppContent, err := os.ReadFile(filepath.Join(basePath, "cpu0", eppFile))
				assert.NoError(t, err)
				assert.Equal(t, eppToSet, string(eppContent))

				// Verify max frequency file
				maxFreqContent, err := os.ReadFile(filepath.Join(basePath, "cpu0", scalingMaxFile))
				assert.NoError(t, err)
				maxFreqInt, _ := strconv.Atoi(strings.TrimSpace(string(maxFreqContent)))
				assert.Equal(t, maxFreqToSet, maxFreqInt)

				// Verify min frequency file
				minFreqContent, err := os.ReadFile(filepath.Join(basePath, "cpu0", scalingMinFile))
				assert.NoError(t, err)
				minFreqInt, _ := strconv.Atoi(strings.TrimSpace(string(minFreqContent)))
				assert.Equal(t, minFreqToSet, minFreqInt)
			},
		},
		{
			name: "successful setDriverValues with empty EPP",
			pstates: &pstatesImpl{
				maxFreq:  intstr.FromInt(maxFreqToSet),
				minFreq:  intstr.FromInt(minFreqToSet),
				governor: governorToSet,
				epp:      "", // Empty EPP should not write to file
			},
			expectError: false,
			validateFiles: func(t *testing.T) {
				// Verify governor file
				governorContent, err := os.ReadFile(filepath.Join(basePath, "cpu0", scalingGovFile))
				assert.NoError(t, err)
				assert.Equal(t, governorToSet, string(governorContent))

				// EPP file should remain unchanged from the previous test (which set it to eppToSet)
				eppContent, err := os.ReadFile(filepath.Join(basePath, "cpu0", eppFile))
				assert.NoError(t, err)
				assert.Equal(t, eppToSet, strings.TrimSpace(string(eppContent)))
			},
		},
		{
			name: "successful setDriverValues with percentage frequencies",
			pstates: &pstatesImpl{
				maxFreq:  intstr.FromString("80%"),
				minFreq:  intstr.FromString("40%"),
				governor: governorToSet,
				epp:      eppToSet,
			},
			expectError: false,
			validateFiles: func(t *testing.T) {
				// For percentages: 40% of range (3000-1000) = 800, so min = 1000 + 800 = 1800
				// 80% of range = 1600, so max = 1000 + 1600 = 2600
				expectedMinFreq := minFreqDefault + uint(float64(maxFreqDefault-minFreqDefault)*0.40)
				expectedMaxFreq := minFreqDefault + uint(float64(maxFreqDefault-minFreqDefault)*0.80)

				minFreqContent, err := os.ReadFile(filepath.Join(basePath, "cpu0", scalingMinFile))
				assert.NoError(t, err)
				minFreqInt, _ := strconv.Atoi(strings.TrimSpace(string(minFreqContent)))
				assert.Equal(t, int(expectedMinFreq), minFreqInt)

				maxFreqContent, err := os.ReadFile(filepath.Join(basePath, "cpu0", scalingMaxFile))
				assert.NoError(t, err)
				maxFreqInt, _ := strconv.Atoi(strings.TrimSpace(string(maxFreqContent)))
				assert.Equal(t, int(expectedMaxFreq), maxFreqInt)
			},
		},
		{
			name: "error case - frequency above system maximum",
			pstates: &pstatesImpl{
				maxFreq:  intstr.FromInt(4000), // Above system max of 3000
				minFreq:  intstr.FromInt(minFreqToSet),
				governor: governorToSet,
				epp:      eppToSet,
			},
			expectError:   true,
			errorContains: "setting frequency 1200-4000 aborted as frequency range is min: 1000 max: 3000. resetting to default",
		},
		{
			name: "error case - frequency below system minimum",
			pstates: &pstatesImpl{
				maxFreq:  intstr.FromInt(maxFreqToSet),
				minFreq:  intstr.FromInt(500), // Below system min of 1000
				governor: governorToSet,
				epp:      eppToSet,
			},
			expectError:   true,
			errorContains: "setting frequency 500-2800 aborted as frequency range is min: 1000 max: 3000. resetting to default",
		},
		{
			name: "error case - getFreqsToScale returns error for mismatched frequency types",
			pstates: &pstatesImpl{
				maxFreq:  intstr.FromString("80%"),     // String type
				minFreq:  intstr.FromInt(minFreqToSet), // Int type
				governor: governorToSet,
				epp:      eppToSet,
			},
			expectError:   true,
			errorContains: "failed to get frequencies to scale: min and max frequencies are not of the same type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := cpu.setDriverValues(tt.pstates)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.ErrorContains(t, err, tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
				if tt.validateFiles != nil {
					tt.validateFiles(t)
				}
			}
		})
	}
}

func TestCpuImpl_setDriverValues_FileWriteErrors(t *testing.T) {
	const cpuID = 0

	// Setup test environment
	featureList[FrequencyScalingFeature].err = nil
	featureList[EPPFeature].err = nil
	typeCopy := coreTypes
	coreTypes = CoreTypeList{&CPUFrequencySet{min: 1000, max: 3000}}
	allCPUDefaultPStatesInfoCopy := allCPUDefaultPStatesInfo
	allCPUDefaultPStatesInfo = make([]pstatesImpl, 1)
	allCPUDefaultPStatesInfo[0] = pstatesImpl{
		maxFreq:  intstr.FromInt(3000),
		minFreq:  intstr.FromInt(1000),
		governor: "powersave",
		epp:      "balance_performance",
	}

	defer func() {
		coreTypes = typeCopy
		allCPUDefaultPStatesInfo = allCPUDefaultPStatesInfoCopy
		featureList[FrequencyScalingFeature].err = errUninitialized
		featureList[EPPFeature].err = errUninitialized
	}()

	// Create CPU instance
	poolMock := new(poolMock)
	hostMock := new(hostMock)
	poolMock.On("getHost").Return(hostMock)
	hostMock.On("NumCoreTypes").Return(uint(1))

	cpu := &cpuImpl{
		id:   cpuID,
		core: &cpuCore{id: cpuID, coreType: 0},
		pool: poolMock,
	}

	pstates := &pstatesImpl{
		maxFreq:  intstr.FromInt(2800),
		minFreq:  intstr.FromInt(1200),
		governor: "performance",
		epp:      "balance_power",
	}

	// Test with non-existent directory (should cause file write errors)
	origBasePath := basePath
	basePath = "/non/existent/path"
	defer func() { basePath = origBasePath }()

	err := cpu.setDriverValues(pstates)
	assert.Error(t, err)
	assert.ErrorContains(t, err, "failed to set governor for cpu 0")
}

func TestCpuImpl_updateFrequencies_MultipleCoreTypes(t *testing.T) {
	const (
		cpuID         = 1    // Use CPU 1 which will be E-core type
		pCoreMaxFreq  = 3500 // P-core: 3.5GHz max
		pCoreMinFreq  = 1200 // P-core: 1.2GHz min
		eCoreMaxFreq  = 2400 // E-core: 2.4GHz max
		eCoreMinFreq  = 800  // E-core: 0.8GHz min
		sharedMaxFreq = 2000 // Shared: 2.0GHz max
		sharedMinFreq = 1000 // Shared: 1.0GHz min
		maxFreqToSet  = 2200
		minFreqToSet  = 900
		governorToSet = "performance"
		eppToSet      = "balance_power"
	)

	// Setup test environment with 3 core types
	featureList[FrequencyScalingFeature].err = nil
	featureList[EPPFeature].err = nil
	typeCopy := coreTypes

	// Create 3 different core types: P-core, E-core, Shared
	coreTypes = CoreTypeList{
		&CPUFrequencySet{min: pCoreMinFreq, max: pCoreMaxFreq},   // Type 0: P-core
		&CPUFrequencySet{min: eCoreMinFreq, max: eCoreMaxFreq},   // Type 1: E-core
		&CPUFrequencySet{min: sharedMinFreq, max: sharedMaxFreq}, // Type 2: Shared
	}

	allCPUDefaultPStatesInfoCopy := allCPUDefaultPStatesInfo

	defer func() {
		coreTypes = typeCopy
		allCPUDefaultPStatesInfo = allCPUDefaultPStatesInfoCopy
		featureList[FrequencyScalingFeature].err = errUninitialized
		featureList[EPPFeature].err = errUninitialized
	}()

	teardown := setupCPUScalingTests(map[string]map[string]string{
		"cpu0": { // P-core
			"governor": "powersave",
			"max":      fmt.Sprint(pCoreMaxFreq),
			"min":      fmt.Sprint(pCoreMinFreq),
			"epp":      "balance_performance",
		},
		"cpu1": { // E-core
			"governor": "powersave",
			"max":      fmt.Sprint(eCoreMaxFreq),
			"min":      fmt.Sprint(eCoreMinFreq),
			"epp":      "balance_performance",
		},
		"cpu2": { // Shared
			"governor": "powersave",
			"max":      fmt.Sprint(sharedMaxFreq),
			"min":      fmt.Sprint(sharedMinFreq),
			"epp":      "balance_performance",
		},
	})
	defer teardown()

	// setupCPUScalingTests now automatically initializes allCPUDefaultPStatesInfo for all CPUs

	// Create CPU instance for E-core (CPU 1)
	poolMock := new(poolMock)
	hostMock := new(hostMock)
	poolMock.On("getHost").Return(hostMock)
	hostMock.On("NumCoreTypes").Return(uint(3))

	cpu := &cpuImpl{
		id:   cpuID,
		core: &cpuCore{id: cpuID, coreType: 1}, // E-core type
		pool: poolMock,
	}

	tests := []struct {
		name          string
		powerProfile  Profile
		expectError   bool
		errorContains string
		validateFiles func(t *testing.T)
	}{
		{
			name: "successful updateFrequencies on E-core with valid frequencies",
			powerProfile: &profileImpl{
				name: "test-profile",
				pstates: &pstatesImpl{
					maxFreq:  intstr.FromInt(maxFreqToSet), // 2200 - within E-core range (800-2400)
					minFreq:  intstr.FromInt(minFreqToSet), // 900 - within E-core range
					governor: governorToSet,
					epp:      eppToSet,
				},
				cstates: cstatesImpl{},
			},
			expectError: false,
			validateFiles: func(t *testing.T) {
				// Verify governor file
				governorContent, err := os.ReadFile(filepath.Join(basePath, "cpu1", scalingGovFile))
				assert.NoError(t, err)
				assert.Equal(t, governorToSet, string(governorContent))

				// Verify EPP file
				eppContent, err := os.ReadFile(filepath.Join(basePath, "cpu1", eppFile))
				assert.NoError(t, err)
				assert.Equal(t, eppToSet, string(eppContent))

				// Verify max frequency file
				maxFreqContent, err := os.ReadFile(filepath.Join(basePath, "cpu1", scalingMaxFile))
				assert.NoError(t, err)
				maxFreqInt, _ := strconv.Atoi(strings.TrimSpace(string(maxFreqContent)))
				assert.Equal(t, maxFreqToSet, maxFreqInt)

				// Verify min frequency file
				minFreqContent, err := os.ReadFile(filepath.Join(basePath, "cpu1", scalingMinFile))
				assert.NoError(t, err)
				minFreqInt, _ := strconv.Atoi(strings.TrimSpace(string(minFreqContent)))
				assert.Equal(t, minFreqToSet, minFreqInt)
			},
		},
		{
			name: "successful updateFrequencies with percentage frequencies on E-core",
			powerProfile: &profileImpl{
				name: "percentage-profile",
				pstates: &pstatesImpl{
					maxFreq:  intstr.FromString("75%"), // 75% of E-core range (2400-800) = 1200, so 800 + 1200 = 2000
					minFreq:  intstr.FromString("25%"), // 25% of E-core range = 400, so 800 + 400 = 1200
					governor: governorToSet,
					epp:      eppToSet,
				},
				cstates: cstatesImpl{},
			},
			expectError: false,
			validateFiles: func(t *testing.T) {
				// For percentages: calculate based on allCPUDefaultPStatesInfo[cpuID] range
				cpuMinFreq := uint(allCPUDefaultPStatesInfo[cpuID].minFreq.IntVal)
				cpuMaxFreq := uint(allCPUDefaultPStatesInfo[cpuID].maxFreq.IntVal)
				// 25% = 400, so min = cpuMinFreq + 400 = 800 + 400 = 1200
				// 75% = 1200, so max = cpuMinFreq + 1200 = 800 + 1200 = 2000
				expectedMinFreq := cpuMinFreq + uint(float64(cpuMaxFreq-cpuMinFreq)*0.25)
				expectedMaxFreq := cpuMinFreq + uint(float64(cpuMaxFreq-cpuMinFreq)*0.75)

				minFreqContent, err := os.ReadFile(filepath.Join(basePath, "cpu1", scalingMinFile))
				assert.NoError(t, err)
				minFreqInt, _ := strconv.Atoi(strings.TrimSpace(string(minFreqContent)))
				assert.Equal(t, int(expectedMinFreq), minFreqInt)

				maxFreqContent, err := os.ReadFile(filepath.Join(basePath, "cpu1", scalingMaxFile))
				assert.NoError(t, err)
				maxFreqInt, _ := strconv.Atoi(strings.TrimSpace(string(maxFreqContent)))
				assert.Equal(t, int(expectedMaxFreq), maxFreqInt)
			},
		},
		{
			name:         "uses hardware default frequencies when no power profile set",
			powerProfile: nil, // No profile - should use hardware defaults
			expectError:  false,
			validateFiles: func(t *testing.T) {
				// Should use E-core hardware defaults from allCPUDefaultPStatesInfo[cpuID]
				maxFreqContent, err := os.ReadFile(filepath.Join(basePath, "cpu1", scalingMaxFile))
				assert.NoError(t, err)
				maxFreqInt, _ := strconv.Atoi(strings.TrimSpace(string(maxFreqContent)))
				expectedMaxFreq := int(allCPUDefaultPStatesInfo[cpuID].maxFreq.IntVal)
				assert.Equal(t, expectedMaxFreq, maxFreqInt)

				minFreqContent, err := os.ReadFile(filepath.Join(basePath, "cpu1", scalingMinFile))
				assert.NoError(t, err)
				minFreqInt, _ := strconv.Atoi(strings.TrimSpace(string(minFreqContent)))
				expectedMinFreq := int(allCPUDefaultPStatesInfo[cpuID].minFreq.IntVal)
				assert.Equal(t, expectedMinFreq, minFreqInt)

				// Should also use default governor from allCPUDefaultPStatesInfo[cpuID]
				governorContent, err := os.ReadFile(filepath.Join(basePath, "cpu1", scalingGovFile))
				assert.NoError(t, err)
				expectedGovernor := allCPUDefaultPStatesInfo[cpuID].governor
				assert.Equal(t, expectedGovernor, strings.TrimSpace(string(governorContent)))
			},
		},
		{
			name: "frequency above E-core maximum gets clamped to E-core limits",
			powerProfile: &profileImpl{
				name: "clamp-high-profile",
				pstates: &pstatesImpl{
					maxFreq:  intstr.FromInt(3000), // Above E-core max of 2400 but within system max of 3500
					minFreq:  intstr.FromInt(minFreqToSet),
					governor: governorToSet,
					epp:      eppToSet,
				},
				cstates: cstatesImpl{},
			},
			expectError: false,
			validateFiles: func(t *testing.T) {
				// Max frequency gets clamped to E-core maximum when above CPU max but within system range
				maxFreqContent, err := os.ReadFile(filepath.Join(basePath, "cpu1", scalingMaxFile))
				assert.NoError(t, err)
				maxFreqInt, _ := strconv.Atoi(strings.TrimSpace(string(maxFreqContent)))
				expectedMaxFreq := int(allCPUDefaultPStatesInfo[cpuID].maxFreq.IntVal)
				assert.Equal(t, expectedMaxFreq, maxFreqInt) // Should be clamped to 2400

				// Min frequency stays as requested since it's within E-core range (900 is between 800-2400)
				minFreqContent, err := os.ReadFile(filepath.Join(basePath, "cpu1", scalingMinFile))
				assert.NoError(t, err)
				minFreqInt, _ := strconv.Atoi(strings.TrimSpace(string(minFreqContent)))
				assert.Equal(t, minFreqToSet, minFreqInt) // Should remain 900
			},
		},
		{
			name: "error case - frequency below E-core minimum shows hardware limits are enforced",
			powerProfile: &profileImpl{
				name: "invalid-low-profile",
				pstates: &pstatesImpl{
					maxFreq:  intstr.FromInt(maxFreqToSet),
					minFreq:  intstr.FromInt(500), // Below E-core min of 800
					governor: governorToSet,
					epp:      eppToSet,
				},
				cstates: cstatesImpl{},
			},
			expectError:   true,
			errorContains: "setting frequency 500-2200 aborted as frequency range is min: 800 max: 2400. resetting to default",
		},
		{
			name: "P-core frequencies get clamped to E-core limits when within system range",
			powerProfile: &profileImpl{
				name: "p-core-profile-on-e-core",
				pstates: &pstatesImpl{
					maxFreq:  intstr.FromInt(3200), // Above E-core max (2400) but within system max (3500)
					minFreq:  intstr.FromInt(1000), // Above E-core min (800) and within system range
					governor: governorToSet,
					epp:      eppToSet,
				},
				cstates: cstatesImpl{},
			},
			expectError: false,
			validateFiles: func(t *testing.T) {
				// Max should be clamped to E-core maximum (2400 MHz) when above CPU max but within system range
				maxFreqContent, err := os.ReadFile(filepath.Join(basePath, "cpu1", scalingMaxFile))
				assert.NoError(t, err)
				maxFreqInt, _ := strconv.Atoi(strings.TrimSpace(string(maxFreqContent)))
				expectedMaxFreq := int(allCPUDefaultPStatesInfo[cpuID].maxFreq.IntVal)
				assert.Equal(t, expectedMaxFreq, maxFreqInt) // Should be clamped to 2400

				// Min stays as requested since 1000 > E-core min (800), so no clamping needed
				minFreqContent, err := os.ReadFile(filepath.Join(basePath, "cpu1", scalingMinFile))
				assert.NoError(t, err)
				minFreqInt, _ := strconv.Atoi(strings.TrimSpace(string(minFreqContent)))
				assert.Equal(t, 1000, minFreqInt) // Should remain 1000
			},
		},
		{
			name: "error case - frequency outside system range still errors",
			powerProfile: &profileImpl{
				name: "invalid-outside-system-range",
				pstates: &pstatesImpl{
					maxFreq:  intstr.FromInt(4000), // Above system max of 3500
					minFreq:  intstr.FromInt(minFreqToSet),
					governor: governorToSet,
					epp:      eppToSet,
				},
				cstates: cstatesImpl{},
			},
			expectError:   true,
			errorContains: "setting frequency 900-4000 aborted as frequency range is min: 800 max: 2400. resetting to default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset the existing pool mock and set up expectations for this test
			poolMock.ExpectedCalls = poolMock.ExpectedCalls[:0] // Clear all expectations
			poolMock.On("GetPowerProfile").Return(tt.powerProfile).Once()
			poolMock.On("getHost").Return(hostMock).Maybe()

			// Call updateFrequencies which will get the profile from pool and apply it
			err := cpu.updateFrequencies()

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.ErrorContains(t, err, tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
				if tt.validateFiles != nil {
					tt.validateFiles(t)
				}
			}

			// Verify the pool mock was called appropriately
			poolMock.AssertExpectations(t)
		})
	}
}
