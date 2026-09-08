package power

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func setupCPUCStatesTests(cpufiles map[string]map[string]map[string]string) func() {
	origBasePath := basePath
	basePath = "testing/cpus"

	origGetNumOfCpusFunc := getNumberOfCpus
	getNumberOfCpus = func() uint {
		if _, ok := cpufiles["Driver"]; ok {
			return uint(len(cpufiles) - 1)
		} else {
			return uint(len(cpufiles))
		}
	}

	featureList[CStatesFeature].err = nil
	for cpu, states := range cpufiles {
		if cpu == "Driver" {
			err := os.MkdirAll(filepath.Join(basePath, strings.Split(cStatesDrvPath, "/")[0]), os.ModePerm)
			if err != nil {
				panic(err)
			}
			for driver := range states {
				err := os.WriteFile(filepath.Join(basePath, cStatesDrvPath), []byte(driver), 0o644)
				if err != nil {
					panic(err)
				}
				break
			}
			continue
		}
		cpuStatesDir := filepath.Join(basePath, cpu, cStatesDir)
		err := os.MkdirAll(cpuStatesDir, os.ModePerm)
		if err != nil {
			panic(err)
		}
		for state, props := range states {
			_ = os.Mkdir(filepath.Join(cpuStatesDir, state), os.ModePerm)
			for propFile, value := range props {
				err := os.WriteFile(filepath.Join(cpuStatesDir, state, propFile), []byte(value), 0o644)
				if err != nil {
					panic(err)
				}
			}
		}
	}

	return func() {
		err := os.RemoveAll(strings.Split(basePath, "/")[0])
		if err != nil {
			panic(err)
		}
		basePath = origBasePath
		getNumberOfCpus = origGetNumOfCpusFunc
		allCPUCStatesInfo = map[uint]cpuCStatesInfo{}
		featureList[CStatesFeature].err = errUninitialized
	}
}

func Test_mapAvailableCStates(t *testing.T) {
	// Test success case
	states := map[string]map[string]string{
		"state0":   {"name": "C0", "latency": "1", "default_status": "enabled"},
		"state1":   {"name": "C1", "latency": "10", "default_status": "enabled"},
		"state2":   {"name": "C2", "latency": "150", "default_status": "disabled"},
		"state3":   {"name": "POLL", "latency": "0", "default_status": "enabled"},
		"notState": nil,
	}
	cpufiles := map[string]map[string]map[string]string{
		"cpu0": states,
		"cpu1": states,
	}
	teardown := setupCPUCStatesTests(cpufiles)

	err := mapAvailableCStates()
	assert.NoError(t, err)

	expectedMap := map[string]cstateInfo{
		"C0":   {StateNumber: 0, Latency: 1, DefaultStatus: true},
		"C1":   {StateNumber: 1, Latency: 10, DefaultStatus: true},
		"C2":   {StateNumber: 2, Latency: 150, DefaultStatus: false},
		"POLL": {StateNumber: 3, Latency: 0, DefaultStatus: true},
	}
	assert.Equal(t, expectedMap, allCPUCStatesInfo[0])
	assert.Equal(t, expectedMap, allCPUCStatesInfo[1])
	teardown()

	// Test missing name file
	states["state0"] = nil
	teardown = setupCPUCStatesTests(cpufiles)
	err = mapAvailableCStates()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "could not read cpu0 C-State 0 name")
	teardown()

	// Test missing latency file
	states["state0"] = map[string]string{"name": "C0"} // No latency file
	teardown = setupCPUCStatesTests(cpufiles)

	err = mapAvailableCStates()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "could not read cpu0 C-State 0 latency")
	teardown()
}

func TestCStates_preCheckCStates(t *testing.T) {
	teardown := setupCPUCStatesTests(map[string]map[string]map[string]string{
		"cpu0":   nil,
		"Driver": {"intel_idle\n": nil},
	})
	defer teardown()
	state := initCStates()
	assert.Equal(t, "C-States", state.name)
	assert.Equal(t, "intel_idle", state.driver)
	assert.Nil(t, state.FeatureError())
	teardown()

	teardown = setupCPUCStatesTests(map[string]map[string]map[string]string{
		"Driver": {"something": nil},
	})
	feature := initCStates()
	assert.ErrorContains(t, feature.FeatureError(), "unsupported")
	assert.Equal(t, "something", feature.driver)
	teardown()
}

func TestCpuImpl_applyCStates(t *testing.T) {
	states := map[string]map[string]string{
		"state0": {"name": "C0", "disable": "0", "latency": "1"},
		"state2": {"name": "C2", "disable": "0", "latency": "10"},
	}
	cpufiles := map[string]map[string]map[string]string{
		"cpu0": states,
	}
	defer setupCPUCStatesTests(cpufiles)()

	allCPUCStatesInfo[0] = map[string]cstateInfo{
		"C2": {StateNumber: 2, Latency: 10, DefaultStatus: true},
		"C0": {StateNumber: 0, Latency: 1, DefaultStatus: true},
	}
	err := (&cpuImpl{id: 0}).applyCStates(cstatesImpl{
		states: map[string]bool{
			"C0": false,
			"C2": true,
		},
	})

	assert.NoError(t, err)

	stateFilePath := filepath.Join(
		basePath,
		fmt.Sprint("cpu", 0),
		fmt.Sprintf(cStateDisableFileFmt, 0),
	)
	disabled, _ := readStringFromFile(stateFilePath)
	assert.Equal(t, "1", disabled)

	stateFilePath = filepath.Join(
		basePath,
		fmt.Sprint("cpu", 0),
		fmt.Sprintf(cStateDisableFileFmt, 2),
	)
	disabled, _ = readStringFromFile(stateFilePath)
	assert.Equal(t, "0", disabled)
}

func TestValidateCStates(t *testing.T) {
	defer setupCPUCStatesTests(nil)()

	allCPUCStatesInfo[0] = map[string]cstateInfo{
		"C0": {StateNumber: 0, Latency: 0, DefaultStatus: true},
		"C2": {StateNumber: 2, Latency: 10, DefaultStatus: true},
		"C3": {StateNumber: 3, Latency: 100, DefaultStatus: false},
	}

	// Validate cstates with explicit c-state names
	assert.NoError(t, ValidateCStates(map[string]bool{
		"C0": true,
		"C2": false,
	}, nil))
	assert.ErrorContains(t, ValidateCStates(map[string]bool{
		"C9": false,
	}, nil), "does not exist on this system")

	// Validate cstates with max latency
	validMaxLatency := 11
	assert.NoError(t, ValidateCStates(nil, &validMaxLatency))
	invalidMaxLatency := -11
	assert.ErrorContains(t, ValidateCStates(nil, &invalidMaxLatency), "must be a non-negative integer")

	assert.ErrorContains(t, ValidateCStates(map[string]bool{
		"C0": true,
		"C2": false,
	}, &validMaxLatency), "cannot specify both explicit C-state names and latency-based configuration")
}

func TestAvailableCStates(t *testing.T) {
	allCPUCStatesInfo[0] = map[string]cstateInfo{
		"C1": {StateNumber: 1, Latency: 1, DefaultStatus: true},
		"C2": {StateNumber: 2, Latency: 10, DefaultStatus: true},
		"C3": {StateNumber: 3, Latency: 100, DefaultStatus: false},
	}

	assert.ElementsMatch(t, GetAvailableCStates(), []string{"C1", "C2", "C3"})
}

func TestCpuImpl_updateCStates(t *testing.T) {
	core := &cpuImpl{id: 0}
	// cstates feature not supported
	assert.NoError(t, core.updateCStates())

	// Common filesystem setup
	defer setupCPUCStatesTests(map[string]map[string]map[string]string{
		"cpu0": {
			"state0": {"name": "C0", "disable": "0", "latency": "0"},
			"state1": {"name": "C1", "disable": "0", "latency": "1"},
			"state2": {"name": "C2", "disable": "0", "latency": "10"},
			"state3": {"name": "C6", "disable": "1", "latency": "100"},
		},
		"cpu1": {
			"state0": {"name": "C0", "disable": "0", "latency": "0"},
			"state1": {"name": "C1", "disable": "0", "latency": "1"},
			"state2": {"name": "C2", "disable": "0", "latency": "100"},
		},
	})()

	allCPUCStatesInfo = map[uint]cpuCStatesInfo{
		0: {
			"C0": {StateNumber: 0, Latency: 0, DefaultStatus: true},
			"C1": {StateNumber: 1, Latency: 1, DefaultStatus: true},
			"C2": {StateNumber: 2, Latency: 10, DefaultStatus: true},
			"C6": {StateNumber: 3, Latency: 100, DefaultStatus: false},
		},
		1: {
			"C0": {StateNumber: 0, Latency: 0, DefaultStatus: true},
			"C1": {StateNumber: 1, Latency: 1, DefaultStatus: true},
			"C2": {StateNumber: 2, Latency: 100, DefaultStatus: true},
		},
	}

	testcases := []struct {
		name     string
		profile  Profile
		expected map[uint]map[string]string
	}{
		{
			name:    "Configure c-states by name",
			profile: &profileImpl{cstates: cstatesImpl{states: map[string]bool{"C0": true, "C6": true}}},
			expected: map[uint]map[string]string{
				0: {"C0": "0", "C1": "0", "C2": "0", "C6": "0"},
				1: {"C0": "0", "C1": "0", "C2": "0"},
			},
		},
		{
			name:    "Configure c-states by latency",
			profile: &profileImpl{cstates: cstatesImpl{maxLatencyUs: &[]int{10}[0]}},
			expected: map[uint]map[string]string{
				0: {"C0": "0", "C1": "0", "C2": "0", "C6": "1"},
				1: {"C0": "0", "C1": "0", "C2": "1"},
			},
		},
		{
			name:    "Configure c-states by latency (0)",
			profile: &profileImpl{cstates: cstatesImpl{maxLatencyUs: &[]int{0}[0]}},
			expected: map[uint]map[string]string{
				0: {"C0": "0", "C1": "1", "C2": "1", "C6": "1"},
				1: {"C0": "0", "C1": "1", "C2": "1"},
			},
		},
		{
			name:    "No power profile (use defaults)",
			profile: Profile(nil),
			expected: map[uint]map[string]string{
				0: {"C0": "0", "C1": "0", "C2": "0", "C6": "1"},
				1: {"C0": "0", "C1": "0", "C2": "0"},
			},
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			for id, cpuStatesInfo := range allCPUCStatesInfo {
				core := &cpuImpl{id: id}
				pool := new(poolMock)
				pool.On("GetPowerProfile").Return(testcase.profile)
				core.pool = pool
				assert.NoError(t, core.updateCStates())

				for state, expectedValue := range testcase.expected[id] {
					stateNum := cpuStatesInfo[state].StateNumber
					stateFilePath := filepath.Join(
						basePath,
						fmt.Sprint("cpu", id),
						fmt.Sprintf(cStateDisableFileFmt, stateNum))
					value, _ := os.ReadFile(stateFilePath)
					assert.Equal(t, expectedValue, string(value), "C-state %s should be %s", state, expectedValue)
				}
				pool.AssertExpectations(t)
			}
		})
	}
}
