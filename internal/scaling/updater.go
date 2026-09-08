package scaling

import (
	"time"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
)

type CPUScalingUpdater interface {
	Update(opts *CPUScalingOpts) time.Duration
}

type cpuScalingUpdaterImpl struct {
	dpdkClient DPDKTelemetryClient
	logger     logr.Logger
}

func NewCPUScalingUpdater(dpdkClient DPDKTelemetryClient) CPUScalingUpdater {
	updater := &cpuScalingUpdaterImpl{
		dpdkClient: dpdkClient,
		logger:     ctrl.Log.WithName("CPUScalingUpdater"),
	}

	return updater
}

// Update inspects the current state (usage percentage, frequency) for the managed
// CPU and sets a new target frequency when needed, and returns the duration
// until the next update (cooldown or sample period).
func (u *cpuScalingUpdaterImpl) Update(opts *CPUScalingOpts) time.Duration {
	currentUsage, err := u.dpdkClient.GetUsagePercent(opts.CPU.GetID())
	if err != nil {
		if frequencyInAllowedDifference(opts.FallbackFreq, opts) {
			return opts.SamplePeriod
		}

		err = opts.CPU.SetCPUFrequency(uint(opts.FallbackFreq))
		if err != nil {
			u.logger.Error(err, "failed to set fallback frequency", "cpu", opts.CPU.GetID(), "fallback_freq", opts.FallbackFreq)
			return opts.SamplePeriod
		}
		opts.CurrentTargetFrequency = opts.FallbackFreq
		return opts.SamplePeriod
	}

	if currentUsage >= opts.TargetUsage-opts.AllowedUsageDifference &&
		currentUsage <= opts.TargetUsage+opts.AllowedUsageDifference {
		return opts.SamplePeriod
	}

	currentFrequencyUint, err := opts.CPU.GetCurrentCPUFrequency()
	if err != nil {
		if frequencyInAllowedDifference(opts.FallbackFreq, opts) {
			return opts.SamplePeriod
		}

		err = opts.CPU.SetCPUFrequency(uint(opts.FallbackFreq))
		if err != nil {
			u.logger.Error(err, "failed to set fallback frequency", "cpu", opts.CPU.GetID(), "fallback_freq", opts.FallbackFreq)
			return opts.SamplePeriod
		}
		opts.CurrentTargetFrequency = opts.FallbackFreq
		return opts.SamplePeriod
	}
	currentFrequency := int(currentFrequencyUint)

	nextFrequencyFloat :=
		float64(currentFrequency) * (1.0 + (float64(currentUsage)/float64(opts.TargetUsage)-1.0)*opts.ScaleFactor)
	nextFrequency := int(nextFrequencyFloat)

	if nextFrequency < opts.HWMinFrequency {
		nextFrequency = opts.HWMinFrequency
	}
	if nextFrequency > opts.HWMaxFrequency {
		nextFrequency = opts.HWMaxFrequency
	}

	if frequencyInAllowedDifference(nextFrequency, opts) {
		return opts.SamplePeriod
	}

	err = opts.CPU.SetCPUFrequency(uint(nextFrequency))
	if err != nil {
		u.logger.Error(err, "failed to set next frequency", "cpu", opts.CPU.GetID(), "next_freq", nextFrequency)
		return opts.SamplePeriod
	}
	opts.CurrentTargetFrequency = nextFrequency
	u.logger.V(6).Info("set next frequency",
		"cpu", opts.CPU.GetID(),
		"usage", currentUsage,
		"prev_freq", currentFrequency,
		"next_freq", nextFrequency,
	)

	return opts.CooldownPeriod
}

func frequencyInAllowedDifference(frequency int, opts *CPUScalingOpts) bool {
	if opts.CurrentTargetFrequency != FrequencyNotYetSet &&
		frequency >= opts.CurrentTargetFrequency-opts.AllowedFrequencyDifference &&
		frequency <= opts.CurrentTargetFrequency+opts.AllowedFrequencyDifference {
		return true
	}

	return false
}
