package scaling

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type updaterMock struct {
	mock.Mock
}

func (u *updaterMock) Update(opts *CPUScalingOpts) time.Duration {
	return u.Called(opts).Get(0).(time.Duration)
}

type MockDPDKTelemetryClient struct {
	mock.Mock
	DPDKTelemetryClient
}

func (md *MockDPDKTelemetryClient) GetUsagePercent(cpuID uint) (int, error) {
	return md.Called().Get(0).(int), md.Called().Error(1)
}

func TestCPUScalingUpdater_Update(t *testing.T) {
	tCases := []struct {
		testCase     string
		cpuID        uint
		scalingOpts  *CPUScalingOpts
		currentUsage int
		usageErr     error
		currentFreq  string
		expectedFreq string
		nextSetIn    time.Duration
	}{
		{
			testCase: "Test Case 1 - New frequency is set - 1",
			cpuID:    0,
			scalingOpts: &CPUScalingOpts{
				TargetUsage:                80,
				AllowedUsageDifference:     5,
				SamplePeriod:               10 * time.Millisecond,
				CurrentTargetFrequency:     1340980,
				FallbackFreq:               2000000,
				HWMaxFrequency:             3700000,
				HWMinFrequency:             400000,
				ScaleFactor:                1.0,
				AllowedFrequencyDifference: 10000,
				CooldownPeriod:             20 * time.Millisecond,
			},
			currentUsage: 46,
			usageErr:     nil,
			currentFreq:  "1340980",
			expectedFreq: "771063",
			nextSetIn:    20 * time.Millisecond,
		},
		{
			testCase: "Test Case 1 - New frequency is set - 2",
			cpuID:    1,
			scalingOpts: &CPUScalingOpts{
				TargetUsage:                41,
				AllowedUsageDifference:     3,
				SamplePeriod:               10 * time.Millisecond,
				FallbackFreq:               2000000,
				CurrentTargetFrequency:     2134003,
				HWMaxFrequency:             3200000,
				HWMinFrequency:             1000000,
				ScaleFactor:                0.5,
				AllowedFrequencyDifference: 10000,
				CooldownPeriod:             35 * time.Millisecond,
			},
			currentUsage: 74,
			usageErr:     nil,
			currentFreq:  "2134003",
			expectedFreq: "2992809",
			nextSetIn:    35 * time.Millisecond,
		},
		{
			testCase: "Test Case 1 - New frequency is set - 3",
			cpuID:    1,
			scalingOpts: &CPUScalingOpts{
				TargetUsage:                58,
				AllowedUsageDifference:     3,
				SamplePeriod:               10 * time.Millisecond,
				FallbackFreq:               2000000,
				CurrentTargetFrequency:     3024616,
				HWMaxFrequency:             3700000,
				HWMinFrequency:             400000,
				ScaleFactor:                1.9,
				AllowedFrequencyDifference: 10000,
				CooldownPeriod:             35 * time.Millisecond,
			},
			currentUsage: 52,
			usageErr:     nil,
			currentFreq:  "3024616",
			expectedFreq: "2430122",
			nextSetIn:    35 * time.Millisecond,
		},
		{
			testCase: "Test Case 1 - New frequency is set - 4, capped to HWMax",
			cpuID:    1,
			scalingOpts: &CPUScalingOpts{
				TargetUsage:                60,
				AllowedUsageDifference:     5,
				SamplePeriod:               10 * time.Millisecond,
				FallbackFreq:               2000000,
				CurrentTargetFrequency:     2844827,
				HWMaxFrequency:             3200000,
				HWMinFrequency:             1000000,
				ScaleFactor:                1.6,
				AllowedFrequencyDifference: 10000,
				CooldownPeriod:             20 * time.Millisecond,
			},
			currentUsage: 66,
			usageErr:     nil,
			currentFreq:  "2844827",
			expectedFreq: "3200000",
			nextSetIn:    20 * time.Millisecond,
		},
		{
			testCase: "Test Case 1 - New frequency is set - 5, capped to HWMin",
			cpuID:    1,
			scalingOpts: &CPUScalingOpts{
				TargetUsage:                70,
				AllowedUsageDifference:     5,
				SamplePeriod:               10 * time.Millisecond,
				FallbackFreq:               2000000,
				CurrentTargetFrequency:     1260000,
				HWMaxFrequency:             3200000,
				HWMinFrequency:             1000000,
				ScaleFactor:                0.75,
				AllowedFrequencyDifference: 10000,
				CooldownPeriod:             20 * time.Millisecond,
			},
			currentUsage: 50,
			usageErr:     nil,
			currentFreq:  "1260000",
			expectedFreq: "1000000",
			nextSetIn:    20 * time.Millisecond,
		},
		{
			testCase: "Test Case 2 - Usage difference is within accepted range",
			cpuID:    2,
			scalingOpts: &CPUScalingOpts{
				TargetUsage:                80,
				AllowedUsageDifference:     5,
				SamplePeriod:               10 * time.Millisecond,
				FallbackFreq:               2000000,
				CurrentTargetFrequency:     2500000,
				HWMaxFrequency:             3700000,
				HWMinFrequency:             400000,
				ScaleFactor:                1.0,
				AllowedFrequencyDifference: 10000,
				CooldownPeriod:             20 * time.Millisecond,
			},
			currentUsage: 82,
			usageErr:     nil,
			currentFreq:  "2500000",
			expectedFreq: "",
			nextSetIn:    10 * time.Millisecond,
		},
		{
			testCase: "Test Case 3 - Frequency difference is within accepted range",
			cpuID:    2,
			scalingOpts: &CPUScalingOpts{
				TargetUsage:                80,
				AllowedUsageDifference:     1,
				SamplePeriod:               15 * time.Millisecond,
				FallbackFreq:               2000000,
				CurrentTargetFrequency:     3418600,
				HWMaxFrequency:             3700000,
				HWMinFrequency:             400000,
				ScaleFactor:                1.0,
				AllowedFrequencyDifference: 100000,
				CooldownPeriod:             20 * time.Millisecond,
			},
			currentUsage: 78,
			usageErr:     nil,
			currentFreq:  "3607692",
			expectedFreq: "",
			nextSetIn:    15 * time.Millisecond,
		},
		{
			testCase: "Test Case 4 - Current frequency is not yet set",
			cpuID:    2,
			scalingOpts: &CPUScalingOpts{
				TargetUsage:                80,
				AllowedUsageDifference:     1,
				SamplePeriod:               15 * time.Millisecond,
				FallbackFreq:               2000000,
				CurrentTargetFrequency:     FrequencyNotYetSet,
				HWMaxFrequency:             3700000,
				HWMinFrequency:             400000,
				ScaleFactor:                1.0,
				AllowedFrequencyDifference: 100000,
				CooldownPeriod:             20 * time.Millisecond,
			},
			currentUsage: 78,
			usageErr:     nil,
			currentFreq:  "3607692",
			expectedFreq: "3517499",
			nextSetIn:    20 * time.Millisecond,
		},
		{
			testCase: "Test Case 5 - Error getting usage",
			cpuID:    2,
			scalingOpts: &CPUScalingOpts{
				TargetUsage:                80,
				AllowedUsageDifference:     1,
				SamplePeriod:               10 * time.Millisecond,
				FallbackFreq:               2000000,
				CurrentTargetFrequency:     3512500,
				HWMaxFrequency:             3700000,
				HWMinFrequency:             400000,
				ScaleFactor:                1.0,
				AllowedFrequencyDifference: 100000,
				CooldownPeriod:             20 * time.Millisecond,
			},
			currentUsage: 0,
			usageErr:     ErrDPDKMetricMissing,
			currentFreq:  "3512500",
			expectedFreq: "2000000",
			nextSetIn:    10 * time.Millisecond,
		},
		{
			testCase: "Test Case 6 - Error getting usage, fallback frequency is already set",
			cpuID:    2,
			scalingOpts: &CPUScalingOpts{
				TargetUsage:                80,
				AllowedUsageDifference:     1,
				SamplePeriod:               10 * time.Millisecond,
				FallbackFreq:               2000000,
				CurrentTargetFrequency:     2000000,
				HWMaxFrequency:             3700000,
				HWMinFrequency:             400000,
				AllowedFrequencyDifference: 100000,
				ScaleFactor:                1.0,
				CooldownPeriod:             20 * time.Millisecond,
			},
			currentUsage: 0,
			usageErr:     ErrDPDKMetricMissing,
			currentFreq:  "2000000",
			expectedFreq: "",
			nextSetIn:    10 * time.Millisecond,
		},
		{
			testCase: "Test Case 7 - Error getting current frequency",
			cpuID:    2,
			scalingOpts: &CPUScalingOpts{
				TargetUsage:                80,
				AllowedUsageDifference:     1,
				SamplePeriod:               10 * time.Millisecond,
				FallbackFreq:               2000000,
				CurrentTargetFrequency:     3512500,
				HWMaxFrequency:             3700000,
				HWMinFrequency:             400000,
				AllowedFrequencyDifference: 100000,
				ScaleFactor:                1.0,
				CooldownPeriod:             20 * time.Millisecond,
			},
			currentUsage: 70,
			usageErr:     nil,
			currentFreq:  "not-a-frequency",
			expectedFreq: "2000000",
			nextSetIn:    10 * time.Millisecond,
		},
		{
			testCase: "Test Case 8 - Error getting current frequency, fallback frequency is already set",
			cpuID:    2,
			scalingOpts: &CPUScalingOpts{
				TargetUsage:                80,
				AllowedUsageDifference:     1,
				SamplePeriod:               10 * time.Millisecond,
				FallbackFreq:               2000000,
				CurrentTargetFrequency:     2000000,
				HWMaxFrequency:             3700000,
				HWMinFrequency:             400000,
				AllowedFrequencyDifference: 100000,
				ScaleFactor:                1.0,
				CooldownPeriod:             20 * time.Millisecond,
			},
			currentUsage: 70,
			usageErr:     nil,
			currentFreq:  "not-a-frequency",
			expectedFreq: "",
			nextSetIn:    10 * time.Millisecond,
		},
	}

	for _, tc := range tCases {
		t.Run(tc.testCase, func(t *testing.T) {
			// Setup mock filesystem with userspace governor
			host, teardown, err := setupScalingTestFiles(3, map[string]string{
				"governor": "userspace",
				"max":      fmt.Sprintf("%d", tc.scalingOpts.HWMaxFrequency),
				"min":      fmt.Sprintf("%d", tc.scalingOpts.HWMinFrequency),
			})
			assert.NoError(t, err)
			defer teardown()

			cpu := host.GetAllCpus().ByID(tc.cpuID)
			tc.scalingOpts.CPU = cpu

			// Prepare current frequency file and setspeed file paths
			curFreqPath := filepath.Join("testing", "cpus", fmt.Sprintf("cpu%d", tc.cpuID), "cpufreq", "scaling_cur_freq")
			setFreqpath := filepath.Join("testing", "cpus", fmt.Sprintf("cpu%d", tc.cpuID), "cpufreq", "scaling_setspeed")

			// Set current frequency and clear setspeed file
			err = os.WriteFile(curFreqPath, []byte(tc.currentFreq+"\n"), 0o644)
			assert.NoError(t, err)
			err = os.WriteFile(setFreqpath, []byte(""), 0o644)
			assert.NoError(t, err)

			dpdkmock := &MockDPDKTelemetryClient{}
			dpdkmock.On("GetUsagePercent").Return(tc.currentUsage, tc.usageErr)

			// Run updater to set the frequency
			updater := &cpuScalingUpdaterImpl{dpdkClient: dpdkmock}
			nextSetIn := updater.Update(tc.scalingOpts)

			// Verify setspeed matches expected frequency
			frequencyRaw, err := os.ReadFile(setFreqpath)
			assert.NoError(t, err)
			frequency := strings.Trim(string(frequencyRaw), " \n")
			assert.Equal(t, tc.expectedFreq, frequency)
			assert.Equal(t, tc.nextSetIn, nextSetIn)

			dpdkmock.AssertExpectations(t)
		})
	}
}
