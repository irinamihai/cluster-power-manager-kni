package power

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

type mockCPUTopology struct {
	mock.Mock
}

func (m *mockCPUTopology) getID() uint {
	return m.Called().Get(0).(uint)
}

func (m *mockCPUTopology) SetUncore(uncore Uncore) error {
	return m.Called(uncore).Error(0)
}

func (m *mockCPUTopology) applyUncore() error {
	return m.Called().Error(0)
}

func (m *mockCPUTopology) getEffectiveUncore() Uncore {
	ret := m.Called()
	if ret.Get(0) != nil {
		return ret.Get(0).(Uncore)
	}
	return nil
}

func (m *mockCPUTopology) getArchitecture() string {
	ret := m.Called()
	if ret.Get(0) != nil {
		return ret.Get(0).(string)
	}
	return ""
}

func (m *mockCPUTopology) addCPU(u uint) (CPU, error) {
	ret := m.Called(u)

	var r0 CPU
	var r1 error

	if ret.Get(0) != nil {
		r0 = ret.Get(0).(CPU)
	}
	r1 = ret.Error(1)

	return r0, r1
}

func (m *mockCPUTopology) CPUs() *CPUList {
	ret := m.Called()

	var r0 *CPUList
	if ret.Get(0) != nil {
		r0 = ret.Get(0).(*CPUList)
	}

	return r0
}

func (m *mockCPUTopology) Packages() *[]Package {
	ret := m.Called()

	var r0 *[]Package
	if ret.Get(0) != nil {
		r0 = ret.Get(0).(*[]Package)

	}
	return r0
}

func (m *mockCPUTopology) Package(id uint) Package {
	ret := m.Called(id)

	var r0 Package
	if ret.Get(0) != nil {
		r0 = ret.Get(0).(Package)
	}

	return r0
}

type mockCPUPackage struct {
	mock.Mock
}

func (m *mockCPUPackage) getID() uint {
	return m.Called().Get(0).(uint)
}

func (m *mockCPUPackage) SetUncore(uncore Uncore) error {
	return m.Called(uncore).Error(0)
}

func (m *mockCPUPackage) applyUncore() error {
	return m.Called().Error(0)
}

func (m *mockCPUPackage) getEffectiveUncore() Uncore {
	ret := m.Called()
	if ret.Get(0) != nil {
		return ret.Get(0).(Uncore)
	}
	return nil
}

func (m *mockCPUPackage) addCPU(u uint) (CPU, error) {
	ret := m.Called(u)

	var r0 CPU
	var r1 error

	if ret.Get(0) != nil {
		r0 = ret.Get(0).(CPU)
	}
	r1 = ret.Error(1)

	return r0, r1
}

func (m *mockCPUPackage) CPUs() *CPUList {
	ret := m.Called()

	var r0 *CPUList
	if ret.Get(0) != nil {
		r0 = ret.Get(0).(*CPUList)
	}

	return r0
}

func (m *mockCPUPackage) Dies() *[]Die {
	ret := m.Called()

	var r0 *[]Die
	if ret.Get(0) != nil {
		r0 = ret.Get(0).(*[]Die)

	}
	return r0
}

func (m *mockCPUPackage) Die(id uint) Die {
	ret := m.Called(id)

	var r0 Die
	if ret.Get(0) != nil {
		r0 = ret.Get(0).(Die)
	}

	return r0
}

type mockCPUDie struct {
	mock.Mock
}

func (m *mockCPUDie) getID() uint {
	return m.Called().Get(0).(uint)
}

func (m *mockCPUDie) SetUncore(uncore Uncore) error {
	return m.Called(uncore).Error(0)
}

func (m *mockCPUDie) applyUncore() error {
	return m.Called().Error(0)
}

func (m *mockCPUDie) getEffectiveUncore() Uncore {
	ret := m.Called()
	if ret.Get(0) != nil {
		return ret.Get(0).(Uncore)
	}
	return nil
}

func (m *mockCPUDie) addCPU(u uint) (CPU, error) {
	ret := m.Called(u)

	var r0 CPU
	var r1 error

	if ret.Get(0) != nil {
		r0 = ret.Get(0).(CPU)
	}
	r1 = ret.Error(1)

	return r0, r1
}

func (m *mockCPUDie) CPUs() *CPUList {
	ret := m.Called()

	var r0 *CPUList
	if ret.Get(0) != nil {
		r0 = ret.Get(0).(*CPUList)
	}

	return r0
}

func (m *mockCPUDie) Cores() *[]Core {
	ret := m.Called()

	var r0 *[]Core
	if ret.Get(0) != nil {
		r0 = ret.Get(0).(*[]Core)

	}
	return r0
}

func (m *mockCPUDie) Core(id uint) Core {
	ret := m.Called(id)

	var r0 Core
	if ret.Get(0) != nil {
		r0 = ret.Get(0).(Core)
	}

	return r0
}

type mockCPUCore struct {
	mock.Mock
	Core
}

func (m *mockCPUCore) GetType() uint {
	return m.Called().Get(0).(uint)
}

func (m *mockCPUCore) setType(t uint) {

}

func (m *mockCPUCore) addCPU(cpuID uint) (CPU, error) {
	ret := m.Called(cpuID)

	var r0 CPU
	var r1 error

	if ret.Get(0) != nil {
		r0 = ret.Get(0).(CPU)
	}
	r1 = ret.Error(1)

	return r0, r1
}

func (m *mockCPUCore) CPUs() *CPUList {
	ret := m.Called()

	var r0 *CPUList
	if ret.Get(0) != nil {
		r0 = ret.Get(0).(*CPUList)

	}
	return r0
}

func (m *mockCPUCore) getID() uint {
	return m.Called().Get(0).(uint)
}

func setupTopologyTest(cpufiles map[string]map[string]string) func() {
	origBasePath := basePath
	basePath = "testing/cpus"

	// backup pointer to function that gets all CPUs
	// replace it with our controlled function
	origGetNumOfCpusFunc := getNumberOfCpus
	getNumberOfCpus = func() uint { return uint(len(cpufiles)) }

	for cpuName, cpuDetails := range cpufiles {
		cpudir := filepath.Join(basePath, cpuName)
		err := os.MkdirAll(filepath.Join(cpudir, "topology"), os.ModePerm)
		if err != nil {
			panic(err)
		}
		err = os.MkdirAll(filepath.Join(cpudir, "cpufreq"), os.ModePerm)
		if err != nil {
			panic(err)
		}
		for prop, value := range cpuDetails {
			switch prop {
			case "pkg":
				err := os.WriteFile(filepath.Join(cpudir, packageIDFile), []byte(value+"\n"), 0o664)
				if err != nil {
					panic(err)
				}
			case "die":
				err := os.WriteFile(filepath.Join(cpudir, dieIDFile), []byte(value+"\n"), 0o644)
				if err != nil {
					panic(err)
				}
			case "core":
				err := os.WriteFile(filepath.Join(cpudir, coreIDFile), []byte(value+"\n"), 0o644)
				if err != nil {
					panic(err)
				}
			case "max":
				os.WriteFile(filepath.Join(cpudir, cpuMaxFreqFile), []byte(value+"\n"), 0o644)
			case "min":
				os.WriteFile(filepath.Join(cpudir, cpuMinFreqFile), []byte(value+"\n"), 0o644)
			}
		}
	}
	return func() {
		// wipe created cpus dir
		err := os.RemoveAll(strings.Split(basePath, "/")[0])
		if err != nil {
			panic(err)
		}
		// revert cpu /sys path
		basePath = origBasePath
		// revert get number of system cpus function
		getNumberOfCpus = origGetNumOfCpusFunc
	}
}

type topologyTestSuite struct {
	suite.Suite
	origBasePath         string
	origGetNumCpus       func() uint
	origDiscoverTopology func(string) (Topology, error)
}

func TestTopologyDiscovery(t *testing.T) {
	tstSuite := &topologyTestSuite{
		origBasePath:         basePath,
		origGetNumCpus:       getNumberOfCpus,
		origDiscoverTopology: discoverTopology,
	}
	suite.Run(t, tstSuite)
}
func (s *topologyTestSuite) AfterTest(suiteName, testName string) {
	os.RemoveAll(strings.Split(basePath, "/")[0])
	basePath = s.origBasePath
	discoverTopology = s.origDiscoverTopology
	getNumberOfCpus = s.origGetNumCpus
}

func (s *topologyTestSuite) TestCpuImpl_discoverTopology() {
	t := s.T()
	// 2 packages, 1 die, 2 cores, 2 threads, cpus 0,1,4,5 belong to pkg0, 2,3,6,7 to pkg1, 4-7 are hyperthread cpus
	teardown := setupTopologyTest(map[string]map[string]string{
		"cpu0": {
			"pkg":  "0",
			"die":  "0",
			"core": "0",
			"max":  "900000",
			"min":  "10000",
		},
		"cpu1": {
			"pkg":  "0",
			"die":  "0",
			"core": "1",
			"max":  "900000",
			"min":  "10000",
		},
		"cpu2": {
			"pkg":  "1",
			"die":  "0",
			"core": "0",
			"max":  "900000",
			"min":  "10000",
		},
		"cpu3": {
			"pkg":  "1",
			"die":  "0",
			"core": "1",
			"max":  "900000",
			"min":  "10000",
		},
		"cpu4": {
			"pkg":  "0",
			"die":  "0",
			"core": "0",
			"max":  "500000",
			"min":  "10000",
		},
		"cpu5": {
			"pkg":  "0",
			"die":  "0",
			"core": "1",
			"max":  "500000",
			"min":  "10000",
		},
		"cpu6": {
			"pkg":  "1",
			"die":  "0",
			"core": "0",
			"max":  "500000",
			"min":  "10000",
		},
		"cpu7": {
			"pkg":  "1",
			"die":  "0",
			"core": "1",
			"max":  "500000",
			"min":  "10000",
		},
	})
	defer teardown()

	topology, err := discoverTopology("x86_64")
	assert.NoError(t, err)
	topologyObj := topology.(*cpuTopology)

	assert.Len(t, topologyObj.packages, 2)
	assert.Len(t, topologyObj.allCpus, 8)
	assert.ElementsMatch(t, topologyObj.allCpus.IDs(), []uint{0, 1, 2, 3, 4, 5, 6, 7})
	assert.Equal(t, topologyObj.packages[0].(*cpuPackage).id, uint(0))
	assert.Equal(t, topologyObj.packages[1].(*cpuPackage).id, uint(1))

	assert.Len(t, topologyObj.packages[0].(*cpuPackage).dies, 1)
	assert.Len(t, topologyObj.packages[1].(*cpuPackage).dies, 1)
	assert.NotEqual(t, topologyObj.packages[0].(*cpuPackage).dies[0], topologyObj.packages[1].(*cpuPackage).dies[0])
	assert.ElementsMatch(t, topologyObj.packages[0].(*cpuPackage).cpus.IDs(), []uint{0, 1, 4, 5})
	assert.ElementsMatch(t, topologyObj.packages[1].(*cpuPackage).cpus.IDs(), []uint{2, 3, 6, 7})
	// only one die per pkg so pkg cpus == die cpus
	assert.ElementsMatch(t, topologyObj.packages[0].(*cpuPackage).dies[0].(*cpuDie).cpus, topologyObj.packages[0].(*cpuPackage).cpus)
	assert.ElementsMatch(t, topologyObj.packages[1].(*cpuPackage).dies[0].(*cpuDie).cpus, topologyObj.packages[1].(*cpuPackage).cpus)

	// emulate hyperthreading enabled so 2 cpus/threads per physical core
	// without hyperthreading we expect one thread per core
	assert.Len(t, topologyObj.packages[0].(*cpuPackage).dies[0].(*cpuDie).cores, 2)
	assert.Len(t, topologyObj.packages[1].(*cpuPackage).dies[0].(*cpuDie).cores, 2)

	assert.Len(t, topologyObj.packages[0].(*cpuPackage).dies[0].(*cpuDie).cpus, 4)
	assert.Len(t, topologyObj.packages[1].(*cpuPackage).dies[0].(*cpuDie).cpus, 4)

	assert.ElementsMatch(t, topologyObj.packages[0].(*cpuPackage).dies[0].(*cpuDie).cores[0].(*cpuCore).cpus.IDs(), []uint{0, 4})
	assert.ElementsMatch(t, topologyObj.packages[0].(*cpuPackage).dies[0].(*cpuDie).cores[1].(*cpuCore).cpus.IDs(), []uint{1, 5})
	assert.ElementsMatch(t, topologyObj.packages[1].(*cpuPackage).dies[0].(*cpuDie).cores[0].(*cpuCore).cpus.IDs(), []uint{2, 6})
	assert.ElementsMatch(t, topologyObj.packages[1].(*cpuPackage).dies[0].(*cpuDie).cores[1].(*cpuCore).cpus.IDs(), []uint{3, 7})
}

func (s *topologyTestSuite) TestSystemTopology_Getters() {
	cpus := make(CPUList, 2)
	cpus[0] = new(cpuMock)
	cpus[1] = new(cpuMock)

	pkgs := packageList{
		0: &cpuPackage{},
		1: &cpuPackage{},
	}

	topo := &cpuTopology{
		packages: pkgs,
		allCpus:  cpus,
	}

	assert.ElementsMatch(s.T(), *topo.CPUs(), cpus)
	assert.ElementsMatch(s.T(), *topo.Packages(), []Package{pkgs[0], pkgs[1]})
	assert.Equal(s.T(), topo.Package(1), pkgs[1])
	assert.Nil(s.T(), topo.Package(6))
}
func (s *topologyTestSuite) TestSystemTopology_addCpu() {
	defer setupTopologyTest(map[string]map[string]string{})()
	// fail to read fs
	topo := &cpuTopology{
		packages: packageList{},
		allCpus:  make(CPUList, 1),
	}
	cpu, err := topo.addCPU(0)
	assert.Error(s.T(), err)
	assert.Nil(s.T(), cpu)
}

func (s *topologyTestSuite) TestCpuPackage_Getters() {
	cpus := make(CPUList, 2)
	cpus[0] = new(cpuMock)
	cpus[1] = new(cpuMock)

	dice := dieList{
		0: &cpuDie{},
		1: &cpuDie{},
	}

	pkg := &cpuPackage{
		dies: dice,
		cpus: cpus,
	}

	assert.ElementsMatch(s.T(), *pkg.CPUs(), cpus)
	assert.ElementsMatch(s.T(), *pkg.Dies(), []Die{dice[0], dice[1]})
	assert.Equal(s.T(), pkg.Die(1), dice[1])
	assert.Nil(s.T(), pkg.Die(6))
}
func (s *topologyTestSuite) TestCpuPackage_addCpu() {
	defer setupTopologyTest(map[string]map[string]string{})()
	// fail to read fs
	topo := &cpuTopology{
		packages:     packageList{},
		allCpus:      make(CPUList, 1),
		architecture: "x86_64",
	}
	pkg := &cpuPackage{
		topology: topo,
		dies:     dieList{},
		cpus:     make(CPUList, 1),
	}
	cpu, err := pkg.addCPU(0)
	assert.Error(s.T(), err)
	assert.Nil(s.T(), cpu)
}

func (s *topologyTestSuite) TestCpuDie_Getters() {
	cpus := make(CPUList, 2)
	cpus[0] = new(cpuMock)
	cpus[1] = new(cpuMock)

	cores := coreList{
		0: &cpuCore{},
		1: &cpuCore{},
	}

	die := &cpuDie{
		cores: cores,
		cpus:  cpus,
	}

	assert.ElementsMatch(s.T(), *die.CPUs(), cpus)
	assert.ElementsMatch(s.T(), *die.Cores(), []Core{cores[0], cores[1]})
	assert.Equal(s.T(), die.Core(1), cores[1])
	assert.Nil(s.T(), die.Core(6))
}
func (s *topologyTestSuite) TestCpuDie_addCpu() {
	defer setupTopologyTest(map[string]map[string]string{})()
	// fail to read fs
	topo := &cpuTopology{
		packages:     packageList{},
		allCpus:      make(CPUList, 1),
		architecture: "x86_64",
	}
	pkg := &cpuPackage{
		topology: topo,
		dies:     dieList{},
		cpus:     make(CPUList, 1),
	}
	cpu, err := pkg.addCPU(0)
	assert.Error(s.T(), err)
	assert.Nil(s.T(), cpu)
}

func (s *topologyTestSuite) TestCpuCore_Getters() {
	cpus := make(CPUList, 2)
	cpus[0] = new(cpuMock)
	cpus[1] = new(cpuMock)

	core := &cpuCore{
		cpus: cpus,
	}

	assert.ElementsMatch(s.T(), *core.CPUs(), cpus)
}
