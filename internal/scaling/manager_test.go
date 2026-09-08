package scaling

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/cluster-power-manager/cluster-power-manager/internal/power"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap/zapcore"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func createNewCPUScalingManager() cpuScalingManagerImpl {
	log.SetLogger(zap.New(
		zap.UseDevMode(true),
		func(opts *zap.Options) {
			opts.TimeEncoder = zapcore.ISO8601TimeEncoder
		},
	))

	return cpuScalingManagerImpl{
		logger: ctrl.Log.WithName("test-log"),
	}
}

func TestCPUScalingManager_Start(t *testing.T) {
	mgr := createNewCPUScalingManager()
	w := &workerMock{}
	w.On("Stop").Return()
	mgr.workers.Store(uint(0), w)

	ctx, cancel := context.WithCancel(context.TODO())

	cancel()
	mgr.Start(ctx)

	w.AssertCalled(t, "Stop")
}

func TestCPUScalingManager_AddCPUScaling(t *testing.T) {
	origNewCPUScalingWorkerFunc := newCPUScalingWorkerFunc
	t.Cleanup(func() {
		newCPUScalingWorkerFunc = origNewCPUScalingWorkerFunc
	})
	newCPUScalingWorkerFunc = func(
		cpuID uint,
		_ *power.Host,
		dpdkClient DPDKTelemetryClient,
		opts *CPUScalingOpts,
	) CPUScalingWorker {
		return CreateMockWorker(cpuID, opts)
	}

	// Initialize power host and cpus for tests
	host, teardown, err := setupScalingTestFiles(4, map[string]string{
		"governor": "userspace",
		"max":      "3700000",
		"min":      "800000",
	})
	assert.Nil(t, err)
	defer teardown()

	allCpus := host.GetAllCpus()

	tcases := []struct {
		testCase      string
		initialConfig []CPUScalingOpts
		addConfig     []CPUScalingOpts
	}{
		{
			testCase:      "New workers in an empty worker pool",
			initialConfig: []CPUScalingOpts{},
			addConfig: []CPUScalingOpts{
				{CPU: allCpus.ByID(0), SamplePeriod: 10 * time.Millisecond},
				{CPU: allCpus.ByID(1), SamplePeriod: 100 * time.Millisecond},
			},
		},
		{
			testCase: "New workers extending already populated worker pool",
			initialConfig: []CPUScalingOpts{
				{CPU: allCpus.ByID(0), SamplePeriod: 50 * time.Millisecond},
				{CPU: allCpus.ByID(1), SamplePeriod: 500 * time.Millisecond},
			},
			addConfig: []CPUScalingOpts{
				{CPU: allCpus.ByID(2), SamplePeriod: 100 * time.Millisecond},
				{CPU: allCpus.ByID(3), SamplePeriod: 100 * time.Millisecond},
			},
		},
		{
			testCase: "Updating existing workers",
			initialConfig: []CPUScalingOpts{
				{CPU: allCpus.ByID(0), SamplePeriod: 50 * time.Millisecond},
				{CPU: allCpus.ByID(1), SamplePeriod: 500 * time.Millisecond},
			},
			addConfig: []CPUScalingOpts{
				{CPU: allCpus.ByID(0), SamplePeriod: 10 * time.Millisecond},
				{CPU: allCpus.ByID(1), SamplePeriod: 100 * time.Millisecond},
			},
		},
	}

	for _, tc := range tcases {
		t.Run(tc.testCase, func(t *testing.T) {
			mgr := createNewCPUScalingManager()

			// create workers from initial configuration
			initialWorkers := make(map[uint]*workerMock)
			for _, opt := range tc.initialConfig {
				w := CreateMockWorker(opt.CPU.GetID(), &opt)
				initialWorkers[opt.CPU.GetID()] = w
				mgr.workers.Store(opt.CPU.GetID(), w)
			}
			// set up UpdateOpts for workers that will be updated
			for _, opt := range tc.addConfig {
				if w, exists := initialWorkers[opt.CPU.GetID()]; exists {
					w.On("UpdateOpts", &opt).Return()
				}
			}

			mgr.AddCPUScaling(tc.addConfig)

			// assert all added workers exist and have correct values
			for _, opt := range tc.addConfig {
				w, found := mgr.getCPUScalingWorker(opt.CPU.GetID())
				assert.True(t, found)
				typedW := w.(*workerMock)
				assert.Equal(t, &opt, typedW.opts)
			}
			// assert existing workers that were updated got UpdateOpts called
			for _, opt := range tc.addConfig {
				if w, exists := initialWorkers[opt.CPU.GetID()]; exists {
					w.AssertCalled(t, "UpdateOpts", &opt)
				}
			}
			// assert existing workers that were NOT in addConfig are still present and untouched
			for _, opt := range tc.initialConfig {
				cpuID := opt.CPU.GetID()
				_, found := mgr.getCPUScalingWorker(cpuID)
				assert.True(t, found, "initial worker for CPU %d should still exist", cpuID)
				initialWorkers[cpuID].AssertNotCalled(t, "Stop")
			}
		})
	}
}

func TestCPUScalingManager_RemoveCPUScaling(t *testing.T) {
	host, teardown, err := setupScalingTestFiles(4, map[string]string{
		"governor": "userspace",
		"max":      "3700000",
		"min":      "800000",
	})
	assert.Nil(t, err)
	defer teardown()

	allCpus := host.GetAllCpus()

	tcases := []struct {
		testCase      string
		initialConfig []CPUScalingOpts
		removeCPUIDs  []uint
		remainingIDs  []uint
	}{
		{
			testCase: "Removing some workers from existing pool",
			initialConfig: []CPUScalingOpts{
				{CPU: allCpus.ByID(0), SamplePeriod: 10 * time.Millisecond},
				{CPU: allCpus.ByID(1), SamplePeriod: 100 * time.Millisecond},
				{CPU: allCpus.ByID(2), SamplePeriod: 100 * time.Millisecond},
				{CPU: allCpus.ByID(3), SamplePeriod: 100 * time.Millisecond},
			},
			removeCPUIDs: []uint{2, 3},
			remainingIDs: []uint{0, 1},
		},
		{
			testCase: "Removing all workers from existing pool",
			initialConfig: []CPUScalingOpts{
				{CPU: allCpus.ByID(0), SamplePeriod: 10 * time.Millisecond},
				{CPU: allCpus.ByID(1), SamplePeriod: 100 * time.Millisecond},
				{CPU: allCpus.ByID(2), SamplePeriod: 100 * time.Millisecond},
				{CPU: allCpus.ByID(3), SamplePeriod: 100 * time.Millisecond},
			},
			removeCPUIDs: []uint{0, 1, 2, 3},
			remainingIDs: []uint{},
		},
		{
			testCase:      "Removing non-existent CPUs does not panic",
			initialConfig: []CPUScalingOpts{},
			removeCPUIDs:  []uint{99},
			remainingIDs:  []uint{},
		},
	}

	for _, tc := range tcases {
		t.Run(tc.testCase, func(t *testing.T) {
			mgr := createNewCPUScalingManager()

			// create workers from initial configuration
			initialWorkers := make(map[uint]*workerMock)
			for _, opt := range tc.initialConfig {
				w := CreateMockWorker(opt.CPU.GetID(), &opt)
				if slices.Contains(tc.removeCPUIDs, opt.CPU.GetID()) {
					w.On("Stop").Return()
				}
				initialWorkers[opt.CPU.GetID()] = w
				mgr.workers.Store(opt.CPU.GetID(), w)
			}

			mgr.RemoveCPUScaling(tc.removeCPUIDs)

			// assert removed workers were stopped and deleted
			for _, cpuID := range tc.removeCPUIDs {
				_, found := mgr.getCPUScalingWorker(cpuID)
				assert.False(t, found, "worker for CPU %d should be removed", cpuID)
				if w, exists := initialWorkers[cpuID]; exists {
					w.AssertCalled(t, "Stop")
				}
			}
			// assert remaining workers are still present and untouched
			for _, cpuID := range tc.remainingIDs {
				_, found := mgr.getCPUScalingWorker(cpuID)
				assert.True(t, found, "worker for CPU %d should still exist", cpuID)
				initialWorkers[cpuID].AssertNotCalled(t, "Stop")
			}
		})
	}
}
