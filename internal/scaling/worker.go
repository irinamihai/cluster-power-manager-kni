package scaling

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/cluster-power-manager/cluster-power-manager/internal/power"
)

var (
	testHookStopLoop func() bool
)

type CPUScalingWorker interface {
	UpdateOpts(opts *CPUScalingOpts)
	Stop()
}

type cpuScalingWorkerImpl struct {
	cpuID      uint
	opts       atomic.Pointer[CPUScalingOpts]
	cancelFunc func()
	waitGroup  sync.WaitGroup
	updater    CPUScalingUpdater
	logger     logr.Logger
}

func NewCPUScalingWorker(
	cpuID uint,
	powerLib *power.Host,
	dpdkClient DPDKTelemetryClient,
	opts *CPUScalingOpts,
) CPUScalingWorker {
	ctx, cancelFunc := context.WithCancel(context.Background())

	worker := &cpuScalingWorkerImpl{
		cpuID:      cpuID,
		cancelFunc: cancelFunc,
		waitGroup:  sync.WaitGroup{},
		logger:     ctrl.Log.WithName("CPUScalingWorker"),
	}

	worker.opts.Store(opts)
	worker.updater = NewCPUScalingUpdater(dpdkClient)
	worker.waitGroup.Add(1)

	go worker.runLoop(ctx)

	return worker
}

func (w *cpuScalingWorkerImpl) UpdateOpts(opts *CPUScalingOpts) {
	w.opts.Store(opts)
}

func (w *cpuScalingWorkerImpl) Stop() {
	w.cancelFunc()
	w.waitGroup.Wait()
}

// runLoop runs a continuous loop to adjust CPU frequency for a specific CPU
// using CPUScalingUpdater.
func (w *cpuScalingWorkerImpl) runLoop(ctx context.Context) {
	defer w.waitGroup.Done()

	opts := w.opts.Load()
	waitFor := opts.SamplePeriod
	for {
		if testHookStopLoop != nil {
			if testHookStopLoop() {
				return
			}
		}

		opts = w.opts.Load()
		select {
		case <-ctx.Done():
			return
		case <-time.After(waitFor):
			waitFor = w.updater.Update(opts)
		}
	}
}
