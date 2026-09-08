package scaling

import (
	"context"
	"os"
	"sync"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/cluster-power-manager/cluster-power-manager/internal/power"
)

// Func definitions for unit testing
var (
	newCPUScalingWorkerFunc = NewCPUScalingWorker
)

type CPUScalingManager interface {
	manager.Runnable
	AddCPUScaling(optList []CPUScalingOpts)
	RemoveCPUScaling(cpuIDs []uint)
}

type cpuScalingManagerImpl struct {
	powerLibrary *power.Host
	dpdkClient   DPDKTelemetryClient
	workers      sync.Map
	logger       logr.Logger
}

func NewCPUScalingManager(powerLib *power.Host, dpdkClient DPDKTelemetryClient) CPUScalingManager {
	nodeName := os.Getenv("NODE_NAME")

	mgr := &cpuScalingManagerImpl{
		powerLibrary: powerLib,
		dpdkClient:   dpdkClient,
		logger:       ctrl.Log.WithName("CPUScalingManager").WithName(nodeName),
	}

	return mgr
}

func (s *cpuScalingManagerImpl) Start(ctx context.Context) error {
	<-ctx.Done()
	s.stop()
	return nil
}

func (s *cpuScalingManagerImpl) stop() {
	s.logger.V(5).Info("stopping all workers")

	managedCPUs := s.getManagedCPUIDs()

	for _, cpuID := range managedCPUs {
		worker, found := s.workers.LoadAndDelete(cpuID)
		if found {
			worker := worker.(CPUScalingWorker)
			worker.Stop()
			s.logger.V(5).Info("worker stopped successfully", "cpuID", cpuID)
		}
	}

	s.logger.V(5).Info("successfully stopped all")
}

// AddCPUScaling creates or updates per-CPU scaling workers for the given CPUs.
// Existing workers for other CPUs are not affected. Each worker runs continuously
// and tunes the CPU's frequency based on the provided options and real-time usage
// from the DPDK telemetry client.
func (s *cpuScalingManagerImpl) AddCPUScaling(optsList []CPUScalingOpts) {
	for _, opts := range optsList {
		worker, found := s.getCPUScalingWorker(opts.CPU.GetID())
		if !found {
			s.logger.V(5).Info("creating worker", "cpuID", opts.CPU.GetID())
			s.workers.Store(
				opts.CPU.GetID(),
				newCPUScalingWorkerFunc(
					opts.CPU.GetID(),
					s.powerLibrary,
					s.dpdkClient,
					&opts,
				),
			)
		} else {
			worker.UpdateOpts(&opts)
		}
	}
}

// RemoveCPUScaling stops scaling workers for the given CPU IDs.
func (s *cpuScalingManagerImpl) RemoveCPUScaling(cpuIDs []uint) {
	for _, cpuID := range cpuIDs {
		worker, found := s.workers.LoadAndDelete(cpuID)
		if !found {
			s.logger.V(5).Info("worker does not exist", "cpuID", cpuID)
		} else {
			worker := worker.(CPUScalingWorker)
			worker.Stop()
			s.logger.V(5).Info("worker stopped successfully", "cpuID", cpuID)
		}
	}
}

func (s *cpuScalingManagerImpl) getManagedCPUIDs() []uint {
	managedCPUs := make([]uint, 0)
	s.workers.Range(func(key, value any) bool {
		managedCPUs = append(managedCPUs, key.(uint))
		return true
	})

	return managedCPUs
}

func (s *cpuScalingManagerImpl) getCPUScalingWorker(cpuID uint) (CPUScalingWorker, bool) {
	if value, found := s.workers.Load(cpuID); found {
		return value.(CPUScalingWorker), true
	}

	return nil, false
}
