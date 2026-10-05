// this file contains integration tests pof the power library
package power

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const mutationTestTimeout = 5 * time.Second

// notifyingMutex reports lock attempts before waiting for the mutex. The first
// caller can also be held after acquiring it so tests can prove that a second
// mutation waits at the host boundary.
type notifyingMutex struct {
	mutex    sync.Mutex
	attempts chan struct{}
	entered  chan struct{}
	release  chan struct{}
	once     sync.Once
}

func newNotifyingMutex() *notifyingMutex {
	return &notifyingMutex{attempts: make(chan struct{}, 4)}
}

func newGatedNotifyingMutex() *notifyingMutex {
	return &notifyingMutex{
		attempts: make(chan struct{}, 4),
		entered:  make(chan struct{}),
		release:  make(chan struct{}),
	}
}

func (m *notifyingMutex) Lock() {
	select {
	case m.attempts <- struct{}{}:
	default:
	}
	m.mutex.Lock()
	if m.release != nil {
		m.once.Do(func() {
			close(m.entered)
			<-m.release
		})
	}
}

func (m *notifyingMutex) Unlock() {
	m.mutex.Unlock()
}

func waitForSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(mutationTestTimeout):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func waitForResult(t *testing.T, result <-chan error, description string) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(mutationTestTimeout):
		t.Fatalf("timed out waiting for %s", description)
		return nil
	}
}

// TestConcurrentHostMutations proves overlapping host mutations serialize on
// the host mutex.
func TestConcurrentHostMutations(t *testing.T) {
	tests := []struct {
		name string
		run  func(*testing.T)
	}{
		{
			name: "reserved-to-shared move vs SetPowerProfile",
			run:  testReservedToSharedMoveVsSetPowerProfile,
		},
		{
			name: "shared-to-exclusive vs exclusive-to-shared moves",
			run:  testOppositeDirectionPoolMoves,
		},
		{
			name: "shared SetCPUIDs vs exclusive MoveCPUIDs",
			run:  testSetCPUIDsVsMoveCPUIDs,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, tc.run)
	}
}

// Exclusive pool Remove() takes the host mutex for the whole operation,
// including draining CPUs back to shared.
func TestExclusivePoolRemovalUsesHostMutex(t *testing.T) {
	instance := setupIntegrationHost(t, 2)

	pool, err := instance.AddExclusivePool("exclusive")
	require.NoError(t, err)
	require.NoError(t, instance.GetSharedPool().MoveCPUIDs([]uint{0, 1}))
	require.NoError(t, pool.MoveCPUIDs([]uint{0, 1}))
	assert.ElementsMatch(t, []uint{0, 1}, pool.Cpus().IDs())

	// Hold the host mutex before Remove() so the call must block.
	hostMutex := newNotifyingMutex()
	hostMutex.Lock()
	waitForSignal(t, hostMutex.attempts, "test to acquire the host mutex")
	replaceHostMutexForTest(t, instance, hostMutex)

	removeResult := make(chan error, 1)
	go func() { removeResult <- pool.Remove() }()
	waitForSignal(t, hostMutex.attempts, "pool removal to reach the host mutex")
	select {
	case err := <-removeResult:
		t.Fatalf("exclusive pool removal finished while the host mutex was held: %v", err)
	default:
	}
	assert.ElementsMatch(t, []uint{0, 1}, pool.Cpus().IDs())

	hostMutex.Unlock()
	require.NoError(t, waitForResult(t, removeResult, "exclusive pool removal"))
	assert.Nil(t, instance.GetExclusivePool("exclusive"))
	assert.Empty(t, *instance.GetAllExclusivePools())
	assert.ElementsMatch(t, []uint{0, 1}, instance.GetSharedPool().Cpus().IDs())
	assert.Empty(t, *instance.GetReservedPool().Cpus())
}

// reserved→shared MoveCpus vs SetPowerProfile on the shared pool.
func testReservedToSharedMoveVsSetPowerProfile(t *testing.T) {
	const numCpus = 88
	instance := setupIntegrationHost(t, numCpus)
	assert.ElementsMatch(t, *instance.GetReservedPool().Cpus(), *instance.GetAllCpus())
	assert.Empty(t, *instance.GetSharedPool().Cpus())

	profile, err := NewPowerProfile(
		"pwr",
		&intstr.IntOrString{Type: intstr.Int, IntVal: 100},
		&intstr.IntOrString{Type: intstr.Int, IntVal: 1000},
		"performance",
		"performance",
		map[string]bool{"C1": true, "C6": false},
		nil,
	)
	require.NoError(t, err)

	// Gate the host mutex so the first mutation can be held mid-flight.
	hostMutex := newGatedNotifyingMutex()
	replaceHostMutexForTest(t, instance, hostMutex)

	// 1. Start the reserved→shared move and wait until it holds the host mutex.
	moveResult := make(chan error, 1)
	go func() { moveResult <- instance.GetSharedPool().MoveCpus(*instance.GetAllCpus()) }()
	waitForSignal(t, hostMutex.attempts, "reserved-to-shared move to acquire the host mutex")
	waitForSignal(t, hostMutex.entered, "reserved-to-shared move to hold the host mutex")
	select {
	case err := <-moveResult:
		t.Fatalf("reserved-to-shared move finished before SetPowerProfile started: %v", err)
	default:
	}

	// 2. Start SetPowerProfile; it must block on the host mutex.
	profileResult := make(chan error, 1)
	go func() { profileResult <- instance.GetSharedPool().SetPowerProfile(profile) }()
	waitForSignal(t, hostMutex.attempts, "SetPowerProfile to reach the host mutex")
	select {
	case err := <-profileResult:
		t.Fatalf("SetPowerProfile finished while the move still held the host mutex: %v", err)
	default:
	}
	assert.Empty(t, *instance.GetSharedPool().Cpus())

	// 3. Release the move; both complete, then every CPU is in shared with the profile.
	close(hostMutex.release)
	require.NoError(t, waitForResult(t, moveResult, "reserved-to-shared move"))
	require.NoError(t, waitForResult(t, profileResult, "SetPowerProfile"))

	assert.Equal(t, profile, instance.GetSharedPool().GetPowerProfile())
	assert.ElementsMatch(t, *instance.GetAllCpus(), *instance.GetSharedPool().Cpus())
	for i := uint(0); i < numCpus; i++ {
		assert.NoError(t, verifyPowerProfile(i, profile), "cpuid", i)
	}
}

// Shared→exclusive and exclusive→shared moves between the same pools.
func testOppositeDirectionPoolMoves(t *testing.T) {
	instance := setupIntegrationHost(t, 4)
	require.NoError(t, instance.GetSharedPool().MoveCPUIDs([]uint{0, 1, 2, 3}))
	exclusive, err := instance.AddExclusivePool("exclusive")
	require.NoError(t, err)
	require.NoError(t, exclusive.MoveCPUIDs([]uint{2, 3}))
	assert.ElementsMatch(t, []uint{0, 1}, instance.GetSharedPool().Cpus().IDs())
	assert.ElementsMatch(t, []uint{2, 3}, exclusive.Cpus().IDs())

	sharedProfile, err := NewPowerProfile(
		"shared",
		&intstr.IntOrString{Type: intstr.Int, IntVal: 100},
		&intstr.IntOrString{Type: intstr.Int, IntVal: 1000},
		"performance",
		"performance",
		map[string]bool{"C1": true, "C6": false},
		nil,
	)
	require.NoError(t, err)
	exclusiveProfile, err := NewPowerProfile(
		"exclusive",
		&intstr.IntOrString{Type: intstr.Int, IntVal: 200},
		&intstr.IntOrString{Type: intstr.Int, IntVal: 2000},
		"performance",
		"performance",
		map[string]bool{"C1": false, "C6": true},
		nil,
	)
	require.NoError(t, err)
	require.NoError(t, instance.GetSharedPool().SetPowerProfile(sharedProfile))
	require.NoError(t, exclusive.SetPowerProfile(exclusiveProfile))

	// Gate the host mutex so the first mutation can be held mid-flight.
	hostMutex := newGatedNotifyingMutex()
	replaceHostMutexForTest(t, instance, hostMutex)

	// 1. Start shared→exclusive (CPUs 0 and 1) and wait until it holds the host mutex.
	toExclusive := make(chan error, 1)
	go func() { toExclusive <- exclusive.MoveCPUIDs([]uint{0, 1}) }()
	waitForSignal(t, hostMutex.attempts, "shared-to-exclusive move to acquire the host mutex")
	waitForSignal(t, hostMutex.entered, "shared-to-exclusive move to hold the host mutex")
	select {
	case err := <-toExclusive:
		t.Fatalf("shared-to-exclusive move finished before the opposite move started: %v", err)
	default:
	}

	// 2. Start exclusive→shared (CPUs 1, 2, and 3); it must block, and membership is unchanged.
	toShared := make(chan error, 1)
	go func() { toShared <- instance.GetSharedPool().MoveCPUIDs([]uint{1, 2, 3}) }()
	waitForSignal(t, hostMutex.attempts, "exclusive-to-shared move to reach the host mutex")
	select {
	case err := <-toShared:
		t.Fatalf("exclusive-to-shared move finished while the first move still held the host mutex: %v", err)
	default:
	}
	assert.ElementsMatch(t, []uint{0, 1}, instance.GetSharedPool().Cpus().IDs())
	assert.ElementsMatch(t, []uint{2, 3}, exclusive.Cpus().IDs())

	// 3. Release the first move; CPU 1 moves to exclusive and then back to shared.
	close(hostMutex.release)
	require.NoError(t, waitForResult(t, toExclusive, "shared-to-exclusive move"))
	require.NoError(t, waitForResult(t, toShared, "exclusive-to-shared move"))
	assert.ElementsMatch(t, []uint{1, 2, 3}, instance.GetSharedPool().Cpus().IDs())
	assert.ElementsMatch(t, []uint{0}, exclusive.Cpus().IDs())
	assert.NoError(t, verifyPowerProfile(0, exclusiveProfile), "cpuid", 0)
	for _, cpuID := range []uint{1, 2, 3} {
		assert.NoError(t, verifyPowerProfile(cpuID, sharedProfile), "cpuid", cpuID)
	}
}

// Shared SetCPUIDs (full pool rebuild) vs exclusive MoveCPUIDs.
func testSetCPUIDsVsMoveCPUIDs(t *testing.T) {
	instance := setupIntegrationHost(t, 4)
	require.NoError(t, instance.GetSharedPool().MoveCPUIDs([]uint{0, 1}))
	exclusive, err := instance.AddExclusivePool("exclusive")
	require.NoError(t, err)
	assert.ElementsMatch(t, []uint{0, 1}, instance.GetSharedPool().Cpus().IDs())
	assert.ElementsMatch(t, []uint{2, 3}, instance.GetReservedPool().Cpus().IDs())
	assert.Empty(t, exclusive.Cpus().IDs())

	sharedProfile, err := NewPowerProfile(
		"shared-rebuild",
		&intstr.IntOrString{Type: intstr.Int, IntVal: 300},
		&intstr.IntOrString{Type: intstr.Int, IntVal: 3500},
		"performance",
		"performance",
		map[string]bool{"C1": true, "C1E": true, "C6": false},
		nil,
	)
	require.NoError(t, err)
	exclusiveProfile, err := NewPowerProfile(
		"exclusive-move",
		&intstr.IntOrString{Type: intstr.Int, IntVal: 500},
		&intstr.IntOrString{Type: intstr.Int, IntVal: 5000},
		"performance",
		"performance",
		map[string]bool{"C1": false, "C1E": false, "C6": true},
		nil,
	)
	require.NoError(t, err)
	require.NoError(t, instance.GetSharedPool().SetPowerProfile(sharedProfile))
	require.NoError(t, exclusive.SetPowerProfile(exclusiveProfile))

	// Gate the host mutex so the first mutation can be held mid-flight.
	hostMutex := newGatedNotifyingMutex()
	replaceHostMutexForTest(t, instance, hostMutex)

	// 1. Start shared SetCPUIDs (pull reserved 2,3 into shared) and wait until it holds the host mutex.
	setResult := make(chan error, 1)
	go func() { setResult <- instance.GetSharedPool().SetCPUIDs([]uint{0, 1, 2, 3}) }()
	waitForSignal(t, hostMutex.attempts, "shared SetCPUIDs to acquire the host mutex")
	waitForSignal(t, hostMutex.entered, "shared SetCPUIDs to hold the host mutex")
	select {
	case err := <-setResult:
		t.Fatalf("shared SetCPUIDs finished before the exclusive move started: %v", err)
	default:
	}

	// 2. Start exclusive MoveCPUIDs (CPU 0); it must block, and membership is unchanged.
	moveResult := make(chan error, 1)
	go func() { moveResult <- exclusive.MoveCPUIDs([]uint{0}) }()
	waitForSignal(t, hostMutex.attempts, "exclusive MoveCPUIDs to reach the host mutex")
	select {
	case err := <-moveResult:
		t.Fatalf("exclusive MoveCPUIDs finished while SetCPUIDs still held the host mutex: %v", err)
	default:
	}
	assert.ElementsMatch(t, []uint{0, 1}, instance.GetSharedPool().Cpus().IDs())
	assert.ElementsMatch(t, []uint{2, 3}, instance.GetReservedPool().Cpus().IDs())
	assert.Empty(t, exclusive.Cpus().IDs())

	// 3. Release SetCPUIDs; all CPUs move to shared, then CPU 0 moves to exclusive.
	close(hostMutex.release)
	require.NoError(t, waitForResult(t, setResult, "shared SetCPUIDs"))
	require.NoError(t, waitForResult(t, moveResult, "exclusive MoveCPUIDs"))
	assert.ElementsMatch(t, []uint{1, 2, 3}, instance.GetSharedPool().Cpus().IDs())
	assert.ElementsMatch(t, []uint{0}, exclusive.Cpus().IDs())
	assert.Empty(t, instance.GetReservedPool().Cpus().IDs())
	assert.NoError(t, verifyPowerProfile(0, exclusiveProfile), "cpuid", 0)
	for _, cpuID := range []uint{1, 2, 3} {
		assert.NoError(t, verifyPowerProfile(cpuID, sharedProfile), "cpuid", cpuID)
	}
}

func replaceHostMutexForTest(t *testing.T, instance Host, hostMutex sync.Locker) {
	t.Helper()

	host, ok := instance.(*hostImpl)
	require.True(t, ok, "expected the real host implementation")
	host.hostMutex = hostMutex
}

func resetFeatureList() {
	for _, status := range featureList {
		status.err = errUninitialized
	}
}

func setupIntegrationHost(t *testing.T, numCpus int) Host {
	t.Helper()
	t.Cleanup(resetFeatureList)

	cpuConfig := map[string]string{
		"min":                 "11100",
		"max":                 "9990000",
		"driver":              "intel_pstate",
		"available_governors": "performance",
		"epp":                 "performance",
	}
	cpuConfigAll := map[string]map[string]string{}
	cpuTopologyMap := map[string]map[string]string{}
	cpuCstatesMap := map[string]map[string]map[string]string{}
	for i := 0; i < numCpus; i++ {
		cpuConfigAll[fmt.Sprint("cpu", i)] = cpuConfig
		cpuCstatesMap[fmt.Sprint("cpu", i)] = map[string]map[string]string{
			"state0": {"name": "POLL", "disable": "0", "latency": "0"},
			"state1": {"name": "C1", "disable": "0", "latency": "1"},
			"state2": {"name": "C1E", "disable": "0", "latency": "10"},
			"state3": {"name": "C6", "disable": "0", "latency": "170"},
		}
		cpuCstatesMap["Driver"] = map[string]map[string]string{"intel_idle\n": nil}
		cpuTopologyMap[fmt.Sprint("cpu", i)] = map[string]string{
			"pkg":  "0",
			"die":  "0",
			"core": fmt.Sprint(i),
		}
	}

	t.Cleanup(setupCPUCStatesTests(cpuCstatesMap))
	t.Cleanup(setupUncoreTests(map[string]map[string]string{}, ""))
	t.Cleanup(setupCPUScalingTests(cpuConfigAll))
	t.Cleanup(setupTopologyTest(cpuTopologyMap))

	t.Cleanup(PinTestHostIdentity())

	instance, err := CreateInstance("host")
	require.ErrorContainsf(t, err, "intel_uncore_frequency not loaded", "expecting uncore feature error")
	require.NotNil(t, instance)
	require.Len(t, *instance.GetAllCpus(), numCpus)
	return instance
}

// verifies that the cpu is configured correctly
// checking is done relative to basePath
func verifyPowerProfile(cpuID uint, profile Profile) error {
	var allerrs []error
	var err error

	pstates := profile.GetPStates()
	governor, err := readCPUStringProperty(cpuID, scalingGovFile)
	allerrs = append(allerrs, err)
	if governor != pstates.GetGovernor() {
		allerrs = append(allerrs, fmt.Errorf("governor mismatch expected : %s, current %s", pstates.GetGovernor(), governor))
	}

	if pstates.GetEpp() != "" {
		epp, err := readCPUStringProperty(cpuID, eppFile)
		allerrs = append(allerrs, err)
		if epp != pstates.GetEpp() {
			allerrs = append(allerrs, fmt.Errorf("epp mismatch expected : %s, current %s", pstates.GetEpp(), epp))
		}
	}

	maxFreq, err := readCPUUintProperty(cpuID, scalingMaxFile)
	allerrs = append(allerrs, err)
	if maxFreq != uint(pstates.GetMaxFreq().IntVal) {
		allerrs = append(allerrs, fmt.Errorf("maxFreq mismatch expected %d, current %d", pstates.GetMaxFreq().IntVal, maxFreq))
	}
	minFreq, err := readCPUUintProperty(cpuID, scalingMinFile)
	allerrs = append(allerrs, err)
	if minFreq != uint(pstates.GetMinFreq().IntVal) {
		allerrs = append(allerrs, fmt.Errorf("minFreq mismatch expected %d, current %d", pstates.GetMinFreq().IntVal, minFreq))
	}

	for stateName, expected := range profile.GetCStates().States() {
		actual, err := readCPUStringProperty(cpuID, fmt.Sprintf(cStateDisableFileFmt, allCPUCStatesInfo[cpuID][stateName].StateNumber))
		allerrs = append(allerrs, err)

		if expected != (actual == "0") {
			allerrs = append(allerrs, fmt.Errorf("c-state %s mismatch expected %t, current %t", stateName, expected, actual == "0"))
		}
	}

	return errors.Join(allerrs...)
}
