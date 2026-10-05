package power

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"golang.org/x/sys/unix"
	"k8s.io/apimachinery/pkg/util/intstr"
)

type hostMock struct {
	mock.Mock
	hostMutex        sync.Locker
	defaultHostMutex sync.Mutex
}

func (m *hostMock) getHostMutex() sync.Locker {
	if m.hostMutex != nil {
		return m.hostMutex
	}
	return &m.defaultHostMutex
}

func (m *hostMock) Topology() Topology {
	return m.Called().Get(0).(Topology)
}

func (m *hostMock) GetAllExclusivePools() *PoolList {
	return m.Called().Get(0).(*PoolList)
}

func (m *hostMock) SetName(name string) {
	m.Called(name)
}

func (m *hostMock) GetName() string {
	return m.Called().String(0)
}
func (m *hostMock) GetArchitecture() string {
	return m.Called().String(0)
}

func (m *hostMock) GetVendorID() string {
	return m.Called().String(0)
}

func (m *hostMock) NumCoreTypes() uint {
	return m.Called().Get(0).(uint)
}

func (m *hostMock) GetFeaturesInfo() FeatureSet {
	ret := m.Called().Get(0)
	if ret == nil {
		return nil
	} else {
		return ret.(FeatureSet)
	}
}

func (m *hostMock) GetReservedPool() Pool {
	ret := m.Called().Get(0)
	if ret == nil {
		return nil
	} else {
		return ret.(Pool)
	}
}

func (m *hostMock) GetSharedPool() Pool {
	ret := m.Called().Get(0)
	if ret == nil {
		return nil
	} else {
		return ret.(Pool)
	}
}

func (m *hostMock) AddExclusivePool(poolName string) (Pool, error) {
	args := m.Called(poolName)
	retPool := args.Get(0)
	if retPool == nil {
		return nil, args.Error(1)
	} else {
		return retPool.(Pool), args.Error(1)
	}
}

func (m *hostMock) GetExclusivePool(poolName string) Pool {
	ret := m.Called(poolName).Get(0)
	if ret == nil {
		return nil
	} else {
		return ret.(Pool)
	}
}

func (m *hostMock) GetAllCpus() *CPUList {
	ret := m.Called().Get(0)
	if ret == nil {
		return nil
	} else {
		return ret.(*CPUList)
	}
}

func (m *hostMock) GetFreqRanges() CoreTypeList {
	return m.Called().Get(0).(CoreTypeList)
}

func TestHost_initHost(t *testing.T) {
	origGetAllCores := discoverTopology
	defer func() { discoverTopology = origGetAllCores }()

	defer PinTestHostIdentity()()

	const hostName = "host"

	// get topology fail
	discoverTopology = func(string) (Topology, error) { return new(mockCPUTopology), fmt.Errorf("error") }
	host, err := initHost(hostName)
	assert.Nil(t, host)
	assert.Error(t, err)

	core1 := new(cpuMock)
	core1.On("_setPoolProperty", mock.Anything).Return()
	core2 := new(cpuMock)
	core2.On("_setPoolProperty", mock.Anything).Return()

	mockedCores := CPUList{core1, core2}
	topObj := new(mockCPUTopology)
	topObj.On("CPUs").Return(&mockedCores)
	discoverTopology = func(string) (Topology, error) { return topObj, nil }
	host, err = initHost(hostName)

	assert.NoError(t, err)

	core1.AssertExpectations(t)
	core2.AssertExpectations(t)

	hostObj := host.(*hostImpl)
	assert.Equal(t, hostObj.name, hostName)
	assert.Equal(t, hostObj.topology, topObj)
	assert.ElementsMatch(t, hostObj.reservedPool.(*reservedPoolType).cpus, mockedCores)
	assert.NotNil(t, hostObj.sharedPool)
	// both come from PinTestHostIdentity, not the machine running the test;
	// initHost fails outright if either lookup does
	assert.Equal(t, "x86_64", hostObj.GetArchitecture())
	assert.Equal(t, "GenuineIntel", hostObj.GetVendorID())
}

// writeCPUInfo redirects cpuinfo reads at a fixture holding content, for the
// duration of the test.
func writeCPUInfo(t *testing.T, content string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cpuinfo")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	useCPUInfoPath(t, path)
}

// useCPUInfoPath redirects cpuinfo reads at path, which need not exist.
func useCPUInfoPath(t *testing.T, path string) {
	t.Helper()
	original := readCPUInfoField
	readCPUInfoField = func(key string) (string, bool, error) {
		return readCPUInfoFieldFrom(path, key)
	}
	t.Cleanup(func() { readCPUInfoField = original })
}

const x86AMDCPUInfo = `processor	: 0
vendor_id	: AuthenticAMD
cpu family	: 25
model name	: AMD EPYC 9J14 96-Core Processor

processor	: 1
vendor_id	: AuthenticAMD
`

const x86IntelCPUInfo = `processor	: 0
vendor_id	: GenuineIntel
cpu family	: 6
model		: 143
model name	: Intel(R) Xeon(R) Platinum 8480+

processor	: 1
vendor_id	: GenuineIntel
`

const armAmpereCPUInfo = `processor	: 0
BogoMIPS	: 50.00
Features	: fp asimd evtstrm aes
CPU implementer	: 0xc0
CPU architecture: 8
CPU variant	: 0x0
CPU part	: 0xac3
CPU revision	: 1
`

func TestReadCPUInfoField(t *testing.T) {
	t.Run("returns the value for a key", func(t *testing.T) {
		for _, tc := range []struct {
			name, cpuInfo, wantVendor, wantModel string
		}{
			{"AMD", x86AMDCPUInfo, "AuthenticAMD", "AMD EPYC 9J14 96-Core Processor"},
			{"Intel", x86IntelCPUInfo, "GenuineIntel", "Intel(R) Xeon(R) Platinum 8480+"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				writeCPUInfo(t, tc.cpuInfo)

				v, found, err := readCPUInfoField("vendor_id")
				assert.NoError(t, err)
				assert.True(t, found)
				assert.Equal(t, tc.wantVendor, v)

				v, found, err = readCPUInfoField("model name")
				assert.NoError(t, err)
				assert.True(t, found)
				assert.Equal(t, tc.wantModel, v)
			})
		}
	})

	t.Run("returns the first match when a key repeats", func(t *testing.T) {
		// distinct values, or a last-match-wins implementation would be
		// indistinguishable from a correct one
		writeCPUInfo(t, "vendor_id\t: FirstVendor\nvendor_id\t: SecondVendor\n")

		v, found, err := readCPUInfoField("vendor_id")
		assert.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, "FirstVendor", v)
	})

	t.Run("matches keys exactly, not by prefix", func(t *testing.T) {
		// the longer key comes first, so a prefix match for "model" would
		// wrongly return the "model name" value. Real /proc/cpuinfo orders
		// these the other way round, which a prefix match survives.
		writeCPUInfo(t, "model name\t: Some CPU\nmodel\t\t: 143\n")

		v, found, err := readCPUInfoField("model")
		assert.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, "143", v)
	})

	t.Run("splits on the first colon only", func(t *testing.T) {
		writeCPUInfo(t, "model name\t: Some CPU @ 2.60GHz: rev B\n")

		v, found, err := readCPUInfoField("model name")
		assert.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, "Some CPU @ 2.60GHz: rev B", v)
	})

	t.Run("handles a key with no space before the colon", func(t *testing.T) {
		// the arm fixture writes "CPU architecture: 8", unlike the tab-padded
		// keys around it
		writeCPUInfo(t, armAmpereCPUInfo)

		v, found, err := readCPUInfoField("CPU architecture")
		assert.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, "8", v)
	})

	// setVendorID tells "no such field" apart from "cannot read the host" by
	// the found flag alone, so an absent key must not surface as an error.
	t.Run("missing key reports not found, without an error", func(t *testing.T) {
		writeCPUInfo(t, x86AMDCPUInfo)

		v, found, err := readCPUInfoField("CPU implementer")
		assert.NoError(t, err)
		assert.False(t, found)
		assert.Empty(t, v)
	})

	t.Run("unreadable file errors and is not merely not found", func(t *testing.T) {
		useCPUInfoPath(t, filepath.Join(t.TempDir(), "absent"))

		_, found, err := readCPUInfoField("vendor_id")
		assert.Error(t, err)
		assert.False(t, found)
	})
}

func TestHostImpl_setVendorID(t *testing.T) {
	t.Run("x86 vendor passes through verbatim, as lscpu does", func(t *testing.T) {
		// deliberately the raw vendor strings, not normalized to AMD/Intel
		for _, tc := range []struct {
			name, cpuInfo, want string
		}{
			{"AMD", x86AMDCPUInfo, "AuthenticAMD"},
			{"Intel", x86IntelCPUInfo, "GenuineIntel"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				writeCPUInfo(t, tc.cpuInfo)

				host := &hostImpl{architecture: "x86_64"}
				assert.NoError(t, host.setVendorID())
				assert.Equal(t, tc.want, host.GetVendorID())
			})
		}
	})

	t.Run("arm implementer is decoded to a name", func(t *testing.T) {
		writeCPUInfo(t, armAmpereCPUInfo)

		host := &hostImpl{architecture: "aarch64"}
		assert.NoError(t, host.setVendorID())
		assert.Equal(t, "Ampere", host.GetVendorID())
	})

	t.Run("unknown arm implementer falls back rather than failing", func(t *testing.T) {
		writeCPUInfo(t, "CPU implementer\t: 0x2a\n")

		host := &hostImpl{architecture: "aarch64"}
		assert.NoError(t, host.setVendorID())
		assert.Equal(t, "unknown (0x2a)", host.GetVendorID())
	})

	t.Run("malformed arm implementer errors", func(t *testing.T) {
		writeCPUInfo(t, "CPU implementer\t: notahexnumber\n")

		host := &hostImpl{architecture: "aarch64"}
		assert.ErrorContains(t, host.setVendorID(), "malformed CPU implementer")
	})

	// An absent vendor field is not a failure: the host is readable, its vendor
	// simply is not identifiable. Only a read failure is an error, so initHost
	// can treat any error from setVendorID as fatal.
	t.Run("missing field reports unknown and succeeds", func(t *testing.T) {
		for _, tc := range []struct {
			name, arch, cpuInfo string
		}{
			// an aarch64 cpuinfo has no vendor_id, and vice versa
			{"x86 without vendor_id", "x86_64", armAmpereCPUInfo},
			{"arm without CPU implementer", "aarch64", x86AMDCPUInfo},
		} {
			t.Run(tc.name, func(t *testing.T) {
				writeCPUInfo(t, tc.cpuInfo)

				host := &hostImpl{architecture: tc.arch}
				assert.NoError(t, host.setVendorID())
				assert.Equal(t, "unknown", host.GetVendorID())
			})
		}
	})

	t.Run("unreadable cpuinfo errors", func(t *testing.T) {
		for _, arch := range []string{"x86_64", "aarch64"} {
			t.Run(arch, func(t *testing.T) {
				useCPUInfoPath(t, filepath.Join(t.TempDir(), "absent"))

				host := &hostImpl{architecture: arch}
				assert.Error(t, host.setVendorID())
				assert.Empty(t, host.GetVendorID())
			})
		}
	})
}

// The Utsname here is built by the test, never read from the machine running
// it: uname reports "arm64" on Darwin where Linux reports "aarch64", so any
// assertion against the real host is only true on some hosts.
func TestMachineName(t *testing.T) {
	for _, want := range []string{"x86_64", "aarch64"} {
		t.Run(want, func(t *testing.T) {
			var uts unix.Utsname
			// leave the remaining bytes NUL, as the kernel does
			copy(uts.Machine[:], want)

			assert.Equal(t, want, machineName(&uts))
		})
	}
}

func TestHostImpl_setArchitecture(t *testing.T) {
	t.Run("stores the reported architecture", func(t *testing.T) {
		stubHostArchitecture(t, "aarch64", nil)

		host := &hostImpl{}
		require.NoError(t, host.setArchitecture())
		assert.Equal(t, "aarch64", host.GetArchitecture())
	})

	t.Run("wraps a lookup failure and stores nothing", func(t *testing.T) {
		stubHostArchitecture(t, "", errors.New("uname exploded"))

		host := &hostImpl{}
		err := host.setArchitecture()
		assert.ErrorContains(t, err, "uname exploded")
		assert.Empty(t, host.GetArchitecture())
	})
}

// stubHostArchitecture makes architecture detection return arch and err for the
// duration of the test, so nothing reads the machine running it.
func stubHostArchitecture(t *testing.T, arch string, err error) {
	t.Helper()
	original := getHostArchitecture
	getHostArchitecture = func() (string, error) { return arch, err }
	t.Cleanup(func() { getHostArchitecture = original })
}

func TestHostImpl_AddExclusivePool(t *testing.T) {
	// happy path
	poolName := "poolName"
	host := &hostImpl{hostMutex: &sync.Mutex{}}

	pool, err := host.AddExclusivePool(poolName)
	assert.Nil(t, err)

	poolObj := pool.(*exclusivePoolType)
	assert.Contains(t, host.exclusivePools, pool)
	assert.Equal(t, poolObj.name, poolName)
	assert.Equal(t, poolObj.host, host)
	assert.Empty(t, poolObj.cpus)

	// already exists
	returnedPool, err := host.AddExclusivePool(poolName)
	assert.Equal(t, pool, returnedPool)
	assert.Error(t, err)
}

type hostTestsSuite struct {
	suite.Suite
}

func TestHost(t *testing.T) {
	suite.Run(t, new(hostTestsSuite))
}
func (s *hostTestsSuite) TestRemoveExclusivePool() {
	// happy path
	p1 := new(poolMock)
	p1.On("Name").Return("pool1")
	p1.On("Remove").Return(nil)

	p2 := new(poolMock)
	p2.On("name").Return("pool2")
	p2.On("Remove").Return(nil)
	host := &hostImpl{
		exclusivePools: []Pool{p1, p2},
	}
	s.NoError(host.GetAllExclusivePools().remove(p1))
	s.Assert().NotContains(host.exclusivePools, p1)
	s.Assert().Contains(host.exclusivePools, p2)

	// not existing
	p3 := new(poolMock)
	p3.On("Name").Return("pool3")
	p3.On("Remove").Return(nil)
	s.Error(new(hostImpl).GetAllExclusivePools().remove(p3))
}

func (s *hostTestsSuite) TestHostImpl_SetReservedPoolCores() {
	cores := make(CPUList, 4)
	topology := new(mockCPUTopology)
	host := &hostImpl{topology: topology, hostMutex: &sync.Mutex{}}
	for i := range cores {
		m := new(mockCPUCore)
		core, err := newCPU(uint(i), m)
		s.Nil(err)

		cores[i] = core
	}
	topology.On("CPUs").Return(&cores)
	host.reservedPool = &reservedPoolType{poolImpl{host: host, cpus: make(CPUList, 0)}}
	host.sharedPool = &sharedPoolType{poolImpl{powerProfile: &profileImpl{}, host: host, cpus: cores}}

	for _, core := range cores {
		core._setPoolProperty(host.sharedPool)
	}
	referenceCores := make(CPUList, 4)
	copy(referenceCores, cores)
	s.Nil(host.GetReservedPool().setCpus(referenceCores))
	s.ElementsMatch(host.GetReservedPool().Cpus().IDs(), referenceCores.IDs())
	s.Len(host.GetSharedPool().Cpus().IDs(), 0)

}

func (s *hostTestsSuite) TestAddSharedPool() {
	cores := make(CPUList, 4)
	topology := new(mockCPUTopology)
	host := &hostImpl{topology: topology, hostMutex: &sync.Mutex{}}
	host.sharedPool = &sharedPoolType{poolImpl{powerProfile: &profileImpl{}, host: host}}
	for i := range cores {
		m := new(mockCPUCore)
		core, err := newCPU(uint(i), m)
		s.Nil(err)

		cores[i] = core
	}
	topology.On("CPUs").Return(&cores)

	host.reservedPool = &reservedPoolType{poolImpl{host: host, cpus: cores}}
	for _, core := range cores {
		core._setPoolProperty(host.reservedPool)
	}

	referenceCores := make(CPUList, 2)
	copy(referenceCores, cores[0:2])
	s.Nil(host.GetSharedPool().setCpus(referenceCores))

	s.ElementsMatch(host.sharedPool.Cpus().IDs(), referenceCores.IDs())
}

func (s *hostTestsSuite) TestRemoveCoreFromExclusivePool() {
	pool := &poolImpl{
		name:         "test",
		powerProfile: &profileImpl{},
	}
	cores := make(CPUList, 4)
	for i := range cores {
		m := new(mockCPUCore)
		core, err := newCPU(uint(i), m)
		s.Nil(err)

		cores[i] = core
	}
	pool.cpus = cores

	topology := new(mockCPUTopology)

	host := &hostImpl{
		name:           "test_host",
		exclusivePools: []Pool{pool},
		topology:       topology,
		hostMutex:      &sync.Mutex{},
	}
	pool.host = host
	for _, core := range cores {
		core._setPoolProperty(host.exclusivePools[0])
	}

	host.sharedPool = &sharedPoolType{poolImpl{powerProfile: &profileImpl{}, host: host}}

	coresToRemove := make(CPUList, 2)
	copy(coresToRemove, cores[0:2])
	coresToPreserve := make(CPUList, 2)
	copy(coresToPreserve, cores[2:])
	s.Nil(host.GetSharedPool().MoveCpus(coresToRemove))

	s.ElementsMatch(host.GetExclusivePool("test").Cpus().IDs(), coresToPreserve.IDs())
	s.ElementsMatch(host.GetSharedPool().Cpus().IDs(), coresToRemove.IDs())

}

func (s *hostTestsSuite) TestAddCoresToExclusivePool() {
	topology := new(mockCPUTopology)
	host := &hostImpl{
		topology:  topology,
		hostMutex: &sync.Mutex{},
	}
	host.exclusivePools = []Pool{&exclusivePoolType{poolImpl{
		name:         "test",
		cpus:         make([]CPU, 0),
		powerProfile: &profileImpl{},
		host:         host,
	}}}
	host.name = "test_node"
	cores := make(CPUList, 4)
	for i := range cores {
		m := new(mockCPUCore)
		core, err := newCPU(uint(i), m)
		s.Nil(err)

		cores[i] = core
	}
	topology.On("CPUs").Return(&cores)
	host.sharedPool = &sharedPoolType{poolImpl{powerProfile: &profileImpl{}, host: host, cpus: cores}}
	for _, core := range cores {
		core._setPoolProperty(host.sharedPool)
	}

	var movedCoresIds []uint
	for _, core := range cores[:2] {
		movedCoresIds = append(movedCoresIds, core.GetID())
	}
	s.Nil(host.GetExclusivePool("test").MoveCPUIDs(movedCoresIds))
	unmoved := cores[2:]
	s.ElementsMatch(host.GetSharedPool().Cpus().IDs(), unmoved.IDs())
	s.Len(host.GetExclusivePool("test").Cpus().IDs(), 2)

}

// //
func (s *hostTestsSuite) TestUpdateProfile() {
	profile := &profileImpl{name: "powah", pstates: &pstatesImpl{minFreq: intstr.FromInt(2500), maxFreq: intstr.FromInt(3200)}, cstates: cstatesImpl{states: map[string]bool{"C1": true}}}
	host := hostImpl{
		sharedPool:    new(poolMock),
		featureStates: &FeatureSet{FrequencyScalingFeature: &featureStatus{err: nil}},
		hostMutex:     &sync.Mutex{},
	}
	origFeatureList := featureList
	featureList = map[featureID]*featureStatus{
		FrequencyScalingFeature: {
			err:      nil,
			initFunc: initScalingDriver,
		},
		CStatesFeature: {
			err:      nil,
			initFunc: initCStates,
		},
	}
	defer func() { featureList = origFeatureList }()
	pool := &poolImpl{name: "ex", powerProfile: profile, host: &host}
	host.exclusivePools = []Pool{pool}
	s.Equal(uint(host.GetExclusivePool("ex").GetPowerProfile().GetPStates().GetMinFreq().IntVal), uint(2500))
	s.Equal(uint(host.GetExclusivePool("ex").GetPowerProfile().GetPStates().GetMaxFreq().IntVal), uint(3200))
	s.Equal(host.GetExclusivePool("ex").GetPowerProfile().GetCStates().States(), map[string]bool{"C1": true})

	newProfile := &profileImpl{name: "powah", pstates: &pstatesImpl{minFreq: intstr.FromInt(1200), maxFreq: intstr.FromInt(2500)}, cstates: cstatesImpl{states: map[string]bool{"C1": false, "C2": true}}}
	s.Nil(host.GetExclusivePool("ex").SetPowerProfile(newProfile))
	s.Equal(uint(host.GetExclusivePool("ex").GetPowerProfile().GetPStates().GetMinFreq().IntVal), uint(1200))
	s.Equal(uint(host.GetExclusivePool("ex").GetPowerProfile().GetPStates().GetMaxFreq().IntVal), uint(2500))
	s.Equal(host.GetExclusivePool("ex").GetPowerProfile().GetCStates().States(), map[string]bool{"C1": false, "C2": true})
	s.Nil(host.GetExclusivePool("ex").GetPowerProfile().GetCStates().GetMaxLatencyUs())

	// Update p-state frequency and c-state configuration by latency
	maxLatencyUs := 10
	newProfile = &profileImpl{name: "powah", pstates: &pstatesImpl{minFreq: intstr.FromInt(2500), maxFreq: intstr.FromInt(3000)}, cstates: cstatesImpl{maxLatencyUs: &maxLatencyUs}}
	s.Nil(host.GetExclusivePool("ex").SetPowerProfile(newProfile))
	s.Equal(uint(host.GetExclusivePool("ex").GetPowerProfile().GetPStates().GetMinFreq().IntVal), uint(2500))
	s.Equal(uint(host.GetExclusivePool("ex").GetPowerProfile().GetPStates().GetMaxFreq().IntVal), uint(3000))
	s.Nil(host.GetExclusivePool("ex").GetPowerProfile().GetCStates().States())
	s.Equal(host.GetExclusivePool("ex").GetPowerProfile().GetCStates().GetMaxLatencyUs(), &maxLatencyUs)
}

func (s *hostTestsSuite) TestRemoveCoresFromSharedPool() {
	topology := new(mockCPUTopology)
	host := &hostImpl{topology: topology, hostMutex: &sync.Mutex{}}
	host.exclusivePools = []Pool{&poolImpl{
		name:         "test",
		cpus:         make([]CPU, 0),
		powerProfile: &profileImpl{},
		host:         host,
	}}
	host.name = "test_node"
	cores := make(CPUList, 4)
	for i := range cores {
		m := new(mockCPUCore)
		core, err := newCPU(uint(i), m)
		s.Nil(err)

		cores[i] = core
	}
	host.sharedPool = &sharedPoolType{poolImpl{powerProfile: &profileImpl{}, host: host, cpus: cores}}
	host.reservedPool = &reservedPoolType{poolImpl{host: host, cpus: make([]CPU, 0)}}

	for _, core := range cores {
		core._setPoolProperty(host.sharedPool)
	}
	coresCopy := make(CPUList, 4)
	copy(coresCopy, cores)
	s.Nil(host.GetReservedPool().MoveCpus(coresCopy))
	s.ElementsMatch(host.GetReservedPool().Cpus().IDs(), coresCopy.IDs())
	s.Len(host.GetSharedPool().Cpus().IDs(), 0)
}

func (s *hostTestsSuite) TestGetExclusivePool() {
	node := &hostImpl{
		exclusivePools: []Pool{
			&poolImpl{name: "p0"},
			&poolImpl{name: "p1"},
			&poolImpl{name: "p2"},
		},
	}
	s.Equal(node.exclusivePools[1], node.GetExclusivePool("p1"))
	s.Nil(node.GetExclusivePool("non existent"))
}
func (s *hostTestsSuite) TestGetSharedPool() {
	cores := make(CPUList, 4)
	for i := range cores {
		m := new(mockCPUCore)
		core, err := newCPU(uint(i), m)
		s.Nil(err)

		cores[i] = core
	}

	node := &hostImpl{
		sharedPool: &sharedPoolType{poolImpl{
			name:         sharedPoolName,
			cpus:         cores,
			powerProfile: &profileImpl{},
		}},
	}
	sharedPool := node.GetSharedPool().(*sharedPoolType)
	s.ElementsMatch(cores.IDs(), sharedPool.cpus.IDs())
	s.Equal(node.sharedPool.(*sharedPoolType).powerProfile, sharedPool.powerProfile)
}
func (s *hostTestsSuite) TestGetReservedPool() {
	cores := make(CPUList, 4)
	for i := range cores {
		m := new(mockCPUCore)
		core, err := newCPU(uint(i), m)
		s.Nil(err)
		cores[i] = core
	}
	poolImp := &poolImpl{
		name:         reservedPoolName,
		cpus:         cores,
		powerProfile: &profileImpl{},
	}
	node := &hostImpl{
		reservedPool: poolImp,
	}
	reservedPool := node.GetReservedPool()
	s.ElementsMatch(cores.IDs(), reservedPool.Cpus().IDs())
	s.Equal(reservedPool.GetPowerProfile(), poolImp.powerProfile)
}
func (s *hostTestsSuite) TestDeleteProfile() {
	allCores := make(CPUList, 12)
	sharedCores := make(CPUList, 4)
	for i := 0; i < 4; i++ {
		m := new(mockCPUCore)
		core, err := newCPU(uint(i), m)
		s.Nil(err)
		allCores[i] = core
		sharedCores[i] = core
	}
	sharedCoresCopy := make(CPUList, len(sharedCores))
	copy(sharedCoresCopy, sharedCores)

	p1cores := make(CPUList, 4)
	for i := 4; i < 8; i++ {
		m := new(mockCPUCore)
		core, err := newCPU(uint(i), m)
		s.Nil(err)
		allCores[i] = core
		p1cores[i-4] = core
	}
	p1copy := make([]CPU, len(p1cores))
	copy(p1copy, p1cores)

	p2cores := make(CPUList, 4)
	for i := 8; i < 12; i++ {
		m := new(mockCPUCore)
		core, err := newCPU(uint(i), m)
		s.Nil(err)
		allCores[i] = core
		p2cores[i-8] = core
	}
	p2copy := make(CPUList, len(p2cores))
	copy(p2copy, p2cores)

	host := &hostImpl{hostMutex: &sync.Mutex{}}
	exclusive := []Pool{
		&exclusivePoolType{poolImpl{
			name:         "pool1",
			cpus:         p1cores,
			powerProfile: &profileImpl{name: "profile1"},
			host:         host,
		}},
		&exclusivePoolType{poolImpl{
			name:         "pool2",
			cpus:         p2cores,
			powerProfile: &profileImpl{name: "profile2"},
			host:         host,
		}},
	}
	shared := &sharedPoolType{poolImpl{
		name:         sharedPoolName,
		cpus:         sharedCores,
		powerProfile: &profileImpl{name: sharedPoolName},
		host:         host,
	}}
	host.exclusivePools = exclusive
	host.sharedPool = shared
	host.reservedPool = &reservedPoolType{poolImpl{host: host}}
	topology := new(mockCPUTopology)
	topology.On("CPUs").Return(&allCores)
	host.topology = topology
	for i := 0; i < 4; i++ {
		sharedCores[i]._setPoolProperty(host.sharedPool)
		p1cores[i]._setPoolProperty(host.exclusivePools[0])
		p2cores[i]._setPoolProperty(host.exclusivePools[1])
	}
	s.NoError(host.GetExclusivePool("pool1").Remove())
	s.Len(host.exclusivePools, 1)
	s.Equal("profile2", host.exclusivePools[0].(*exclusivePoolType).powerProfile.(*profileImpl).name)
	s.ElementsMatch(host.exclusivePools[0].(*exclusivePoolType).cpus, p2copy)
	newShared := make(CPUList, len(sharedCoresCopy))
	copy(newShared, sharedCoresCopy)
	newShared = append(newShared, p1copy...)
	s.ElementsMatch(host.GetSharedPool().Cpus().IDs(), newShared.IDs())
}
