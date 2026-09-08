package scaling

import (
	"time"

	"github.com/cluster-power-manager/cluster-power-manager/internal/power"
)

const FrequencyNotYetSet int = -1

type CPUScalingOpts struct {
	CPU                        power.CPU
	SamplePeriod               time.Duration
	CooldownPeriod             time.Duration
	TargetUsage                int
	AllowedUsageDifference     int
	AllowedFrequencyDifference int
	HWMaxFrequency             int
	HWMinFrequency             int
	CurrentTargetFrequency     int
	ScaleFactor                float64
	FallbackFreq               int
}
