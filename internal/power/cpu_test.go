package power

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type cpuMock struct {
	mock.Mock
}

func (m *cpuMock) _setPoolProperty(pool Pool) {
	m.Called(pool)
}
func (m *cpuMock) consolidate() error {
	return m.Called().Error(0)
}
func (m *cpuMock) consolidateUnsafe() error {
	return m.Called().Error(0)
}
func (m *cpuMock) doSetPool(pool Pool) error {
	return m.Called(pool).Error(0)
}
func (m *cpuMock) GetID() uint {
	args := m.Called()
	return args.Get(0).(uint)
}
func (m *cpuMock) GetAbsMinMax() (uint, uint) {
	args := m.Called()
	return args.Get(0).(uint), args.Get(1).(uint)
}

func (m *cpuMock) getPool() Pool {
	args := m.Called().Get(0)
	if args == nil {
		return nil
	} else {
		return args.(Pool)
	}
}

func (m *cpuMock) GetCore() Core {
	return m.Called().Get(0).(Core)
}

func (m *cpuMock) SetPool(pool Pool) error {
	return m.Called(pool).Error(0)
}

func (m *cpuMock) SetCPUFrequency(frequency uint) error {
	return m.Called(frequency).Error(0)
}

func (m *cpuMock) GetCurrentCPUFrequency() (uint, error) {
	args := m.Called()
	return args.Get(0).(uint), args.Error(1)
}

type mutexMock struct {
	mock.Mock
}

func (m *mutexMock) Lock() {
	m.Called()
}

func (m *mutexMock) Unlock() {
	m.Called()
}

func TestNewCore(t *testing.T) {
	cpufiles := map[string]map[string]string{
		"cpu0": {
			"max": "123",
			"min": "100",
			"epp": "some",
		},
	}
	defer setupCPUScalingTests(cpufiles)()

	// happy path - ensure values from files are read correctly
	core := &cpuCore{}
	cpu, err := newCPU(0, core)
	assert.NoError(t, err)

	assert.NotNil(t, cpu.(*cpuImpl).mutex)
	// we don't want to compare value of new mutex, so we set it to nil
	cpu.(*cpuImpl).mutex = nil
	assert.Equal(t, &cpuImpl{
		id:   0,
		core: core,
	}, cpu)
	// now "break" scaling driver by setting a feature error
	featureList[FrequencyScalingFeature].err = fmt.Errorf("some error")

	cpu, err = newCPU(0, nil)

	assert.NoError(t, err)

	assert.NotNil(t, cpu.(*cpuImpl).mutex)
	// Ensure P-States stuff was never read by ensuring related properties are 0
	cpu.(*cpuImpl).mutex = nil
	assert.Equal(t, &cpuImpl{
		id: 0,
	}, cpu)
}

func TestCpuImpl_SetPool(t *testing.T) {
	// feature errors are set so functions inside consolidate() return without doing anything
	var cpuMutex *mutexMock
	host := new(hostMock)

	sharedPool := new(poolMock)
	sharedPool.On("isExclusive").Return(false)
	sharedPool.On("getHost").Return(host)
	sharedPool.On("Name").Return("shared")
	sharedPoolCores := make(CPUList, 8)
	sharedPool.On("Cpus").Return(&sharedPoolCores)
	sharedPool.On("poolMutex").Return(&sync.Mutex{})

	reservedPool := new(poolMock)
	reservedPool.On("isExclusive").Return(false)
	reservedPool.On("getHost").Return(host)
	reservedPool.On("Name").Return("reserved")
	reservedPoolCores := make(CPUList, 8)
	reservedPool.On("Cpus").Return(&reservedPoolCores)
	reservedPool.On("poolMutex").Return(&sync.Mutex{})

	host.On("GetReservedPool").Return(reservedPool)
	host.On("GetSharedPool").Return(sharedPool)

	exclusivePool1 := new(poolMock)
	exclusivePool1.On("isExclusive").Return(true)
	exclusivePool1.On("getHost").Return(host)
	exclusivePool1.On("Name").Return("excl1")
	exclusivePool1Cores := make(CPUList, 8)
	exclusivePool1.On("Cpus").Return(&exclusivePool1Cores)
	exclusivePool1.On("poolMutex").Return(&sync.Mutex{})

	exclusivePool2 := new(poolMock)
	exclusivePool2.On("isExclusive").Return(true)
	exclusivePool2.On("getHost").Return(host)
	exclusivePool2.On("Name").Return("excl2")
	exclusivePool2Cores := make(CPUList, 8)
	exclusivePool2.On("Cpus").Return(&exclusivePool2Cores)
	exclusivePool2.On("poolMutex").Return(&sync.Mutex{})

	cpu := &cpuImpl{
		id:   0,
		pool: sharedPool,
	}
	// nil pool
	// in this scenario we don't expect lock to be acquired
	cpu.mutex = new(mutexMock)
	assert.ErrorContains(t, cpu.SetPool(nil), "cannot be nil")

	// current == target pool, case 0
	cpuMutex = new(mutexMock)
	cpuMutex.On("Unlock").Return().NotBefore(
		cpuMutex.On("Lock").Return(),
	)
	cpu.mutex = cpuMutex
	assert.NoError(t, cpu.SetPool(sharedPool))
	sharedPool.AssertNotCalled(t, "isExclusive")
	assert.True(t, cpu.pool == sharedPool)
	cpuMutex.AssertExpectations(t)

	// shared to reserved
	cpuMutex = new(mutexMock)
	cpuMutex.On("Unlock").Return().NotBefore(
		cpuMutex.On("Lock").Return(),
	)
	cpu.mutex = cpuMutex
	sharedPoolCores[0] = cpu
	cpu.pool = sharedPool
	assert.NoError(t, cpu.SetPool(reservedPool))
	assert.True(t, cpu.pool == reservedPool)
	cpuMutex.AssertExpectations(t)

	// shared to shared
	cpuMutex = new(mutexMock)
	cpuMutex.On("Unlock").Return().NotBefore(
		cpuMutex.On("Lock").Return(),
	)
	cpu.mutex = cpuMutex
	cpu.pool = sharedPool
	sharedPoolCores[0] = cpu
	assert.NoError(t, cpu.SetPool(sharedPool))
	assert.True(t, cpu.pool == sharedPool)
	cpuMutex.AssertExpectations(t)

	// shared to exclusive
	cpuMutex = new(mutexMock)
	cpuMutex.On("Unlock").Return().NotBefore(
		cpuMutex.On("Lock").Return(),
	)
	cpu.mutex = cpuMutex
	cpu.pool = sharedPool
	sharedPoolCores[0] = cpu
	assert.NoError(t, cpu.SetPool(exclusivePool1))
	assert.True(t, cpu.pool == exclusivePool1)
	cpuMutex.AssertExpectations(t)

	// reserved to reserved
	cpuMutex = new(mutexMock)
	cpuMutex.On("Unlock").Return().NotBefore(
		cpuMutex.On("Lock").Return(),
	)
	cpu.mutex = cpuMutex
	cpu.pool = reservedPool
	reservedPoolCores[0] = cpu
	assert.NoError(t, cpu.SetPool(reservedPool))
	assert.True(t, cpu.pool == reservedPool)
	cpuMutex.AssertExpectations(t)

	// reserved to shared
	cpuMutex = new(mutexMock)
	cpuMutex.On("Unlock").Return().NotBefore(
		cpuMutex.On("Lock").Return(),
	)
	cpu.mutex = cpuMutex
	cpu.pool = reservedPool
	reservedPoolCores[0] = cpu
	assert.NoError(t, cpu.SetPool(sharedPool))
	assert.True(t, cpu.pool == sharedPool)
	cpuMutex.AssertExpectations(t)

	// reserved to exclusive
	cpuMutex = new(mutexMock)
	cpuMutex.On("Unlock").Return().NotBefore(
		cpuMutex.On("Lock").Return(),
	)
	cpu.mutex = cpuMutex
	cpu.pool = reservedPool
	reservedPoolCores[0] = cpu
	assert.ErrorContains(t, cpu.SetPool(exclusivePool1), "reserved to exclusive")
	assert.True(t, cpu.pool == reservedPool)
	cpuMutex.AssertExpectations(t)

	// exclusive to reserved
	cpuMutex = new(mutexMock)
	cpuMutex.On("Unlock").Return().NotBefore(
		cpuMutex.On("Lock").Return(),
	)
	cpu.mutex = cpuMutex
	cpu.pool = exclusivePool1
	exclusivePool1Cores[0] = cpu
	assert.ErrorContains(t, cpu.SetPool(reservedPool), "exclusive to reserved")
	assert.True(t, cpu.pool == exclusivePool1)
	cpuMutex.AssertExpectations(t)

	// exclusive to shared
	cpuMutex = new(mutexMock)
	cpuMutex.On("Unlock").Return().NotBefore(
		cpuMutex.On("Lock").Return(),
	)
	cpu.mutex = cpuMutex
	cpu.pool = exclusivePool1
	exclusivePool1Cores[0] = cpu
	assert.NoError(t, cpu.SetPool(sharedPool))
	assert.True(t, cpu.pool == sharedPool)
	cpuMutex.AssertExpectations(t)

	// exclusive to same exclusive
	cpuMutex = new(mutexMock)
	cpuMutex.On("Unlock").Return().NotBefore(
		cpuMutex.On("Lock").Return(),
	)
	cpu.mutex = cpuMutex
	cpu.pool = exclusivePool1
	exclusivePool1Cores[0] = cpu
	assert.NoError(t, cpu.SetPool(exclusivePool1))
	assert.True(t, cpu.pool == exclusivePool1)
	cpuMutex.AssertExpectations(t)

	// exclusive to another exclusive
	cpuMutex = new(mutexMock)
	cpuMutex.On("Unlock").Return().NotBefore(
		cpuMutex.On("Lock").Return(),
	)
	cpu.mutex = cpuMutex
	cpu.pool = exclusivePool1
	exclusivePool1Cores[0] = cpu
	assert.ErrorContains(t, cpu.SetPool(exclusivePool2), " exclusive to different exclusive")
	assert.True(t, cpu.pool == exclusivePool1)
	cpuMutex.AssertExpectations(t)
}

func TestCpuImpl_doSetPool(t *testing.T) {
	var sourcePool, targetPool *poolMock
	var sourcePoolMutex, targetPoolMutex *mutexMock

	var cpu *cpuImpl
	// happy path
	sourcePool = new(poolMock)
	sourcePool.On("Name").Return("sauce")
	sourcePoolMutex = new(mutexMock)

	sourcePoolMutex.On("Unlock").Return().NotBefore(
		sourcePoolMutex.On("Lock").Return(),
	)
	sourcePool.On("poolMutex").Return(sourcePoolMutex)

	targetPool = new(poolMock)
	targetPool.On("Name").Return("target")
	targetPoolMutex = new(mutexMock)

	targetPoolMutex.On("Unlock").Return().NotBefore(
		targetPoolMutex.On("Lock").Return(),
	)
	targetPool.On("poolMutex").Return(targetPoolMutex)

	cpu = &cpuImpl{
		pool: sourcePool,
	}
	sourcePool.On("Cpus").Return(&CPUList{cpu})
	targetPool.On("Cpus").Return(&CPUList{})

	assert.NoError(t, cpu.doSetPool(targetPool))
	assert.True(t, cpu.pool == targetPool)
	sourcePoolMutex.AssertExpectations(t)
	targetPoolMutex.AssertExpectations(t)

	// remove failure
	sourcePool = new(poolMock)
	sourcePool.On("Name").Return("sauce")
	sourcePoolMutex.On("Unlock").Return().NotBefore(
		sourcePoolMutex.On("Lock").Return(),
	)
	sourcePool.On("poolMutex").Return(sourcePoolMutex)

	targetPool = new(poolMock)
	targetPool.On("Name").Return("target")
	targetPoolMutex = new(mutexMock)
	targetPoolMutex.On("Unlock").Return().NotBefore(
		targetPoolMutex.On("Lock").Return(),
	)
	targetPool.On("poolMutex").Return(targetPoolMutex)

	cpu = &cpuImpl{
		pool: sourcePool,
	}
	sourcePool.On("Cpus").Return(&CPUList{})
	targetPool.On("Cpus").Return(&CPUList{})

	assert.ErrorContains(t, cpu.doSetPool(targetPool), "not in pool")
	assert.True(t, cpu.pool == sourcePool)
	sourcePoolMutex.AssertExpectations(t)
	targetPoolMutex.AssertExpectations(t)
}

func TestCoreList_IDs(t *testing.T) {
	cpus := CPUList{}
	var expectedIDs []uint
	for i := uint(0); i < 5; i++ {
		mockedCore := new(cpuMock)
		mockedCore.On("GetID").Return(i)
		cpus = append(cpus, mockedCore)
		expectedIDs = append(expectedIDs, i)
	}
	assert.ElementsMatch(t, cpus.IDs(), expectedIDs)
}

func TestCoreList_ByID(t *testing.T) {
	// test for quick get to skip iteration over list when index == coreId
	cpus := CPUList{}
	for i := uint(0); i < 5; i++ {
		mockedCore := new(cpuMock)
		mockedCore.On("GetID").Return(i)
		cpus = append(cpus, mockedCore)
	}

	assert.Equal(t, cpus[2], cpus.ByID(2))
	cpus[0].(*cpuMock).AssertNotCalled(t, "GetID")
	cpus[1].(*cpuMock).AssertNotCalled(t, "GetID")

	// test for when index != coreID and have to iterate
	cpus = CPUList{}
	for _, u := range []uint{56, 1, 6, 99, 2, 11} {
		mocked := new(cpuMock)
		mocked.On("GetID").Return(u)
		cpus = append(cpus, mocked)
	}
	assert.Equal(t, cpus[3], cpus.ByID(99))
	assert.Equal(t, cpus[5], cpus.ByID(11))

	// not in list
	assert.Nil(t, cpus.ByID(77))
}

func TestCoreList_ManyByIDs(t *testing.T) {
	cpus := CPUList{}
	for i := uint(0); i < 5; i++ {
		mockedCore := new(cpuMock)
		mockedCore.On("GetID").Return(i)
		cpus = append(cpus, mockedCore)
	}
	returnedList, err := cpus.ManyByIDs([]uint{1, 3})
	assert.ElementsMatch(t, returnedList, []CPU{cpus[1], cpus[3]})
	assert.NoError(t, err)

	// out of range
	_, err = cpus.ManyByIDs([]uint{6})
	assert.Error(t, err)
}
