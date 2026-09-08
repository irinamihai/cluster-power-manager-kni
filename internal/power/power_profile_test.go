package power

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestNewProfile(t *testing.T) {
	// Save and initialize coreTypes for testing
	typeCopy := coreTypes
	coreTypes = CoreTypeList{&CPUFrequencySet{min: 10000, max: 1000000}} // 10MHz - 1GHz
	defer func() { coreTypes = typeCopy }()

	oldgovs := availableGovs
	availableGovs = []string{cpuPolicyPowersave, cpuPolicyPerformance}

	allCPUCStatesInfo[0] = cpuCStatesInfo{
		"C0":  {StateNumber: 0, Latency: 0, DefaultStatus: true},
		"C1":  {StateNumber: 1, Latency: 1, DefaultStatus: true},
		"C1E": {StateNumber: 2, Latency: 10, DefaultStatus: true},
		"C6":  {StateNumber: 3, Latency: 100, DefaultStatus: false},
	}

	profile, err := NewPowerProfile(
		"name",
		&intstr.IntOrString{Type: intstr.String, StrVal: "0%"},
		&intstr.IntOrString{Type: intstr.String, StrVal: "10%"},
		cpuPolicyPowersave,
		"epp",
		map[string]bool{},
		nil,
	)
	assert.ErrorIs(t, err, errUninitialized)
	assert.Nil(t, profile)

	featureList[FrequencyScalingFeature].err = nil
	featureList[EPPFeature].err = nil
	featureList[CStatesFeature].err = nil
	defer func() { featureList[FrequencyScalingFeature].err = errUninitialized }()
	defer func() { featureList[EPPFeature].err = errUninitialized }()
	defer func() { featureList[CStatesFeature].err = errUninitialized }()
	defer func() { availableGovs = oldgovs }()

	profile, err = NewPowerProfile(
		"name",
		nil,
		&intstr.IntOrString{Type: intstr.Int, IntVal: 100},
		cpuPolicyPowersave,
		"epp",
		map[string]bool{"C1": true, "C6": false},
		nil,
	)
	assert.NoError(t, err)
	assert.Equal(t, "name", profile.Name())
	assert.Equal(t, uint(10*1000), uint(profile.GetPStates().GetMinFreq().IntVal))
	assert.Equal(t, uint(100*1000), uint(profile.GetPStates().GetMaxFreq().IntVal))
	assert.Equal(t, "powersave", profile.GetPStates().GetGovernor())
	assert.Equal(t, "epp", profile.GetPStates().GetEpp())
	assert.Equal(t, map[string]bool{"C1": true, "C6": false}, profile.GetCStates().States())
	assert.Nil(t, profile.GetCStates().GetMaxLatencyUs())

	maxLatency := 10
	profile, err = NewPowerProfile(
		"name",
		nil,
		&intstr.IntOrString{Type: intstr.Int, IntVal: 100},
		cpuPolicyPerformance,
		cpuPolicyPerformance,
		nil,
		&maxLatency,
	)
	assert.NoError(t, err)
	assert.Equal(t, "name", profile.Name())
	assert.Equal(t, uint(10*1000), uint(profile.GetPStates().GetMinFreq().IntVal))
	assert.Equal(t, uint(100*1000), uint(profile.GetPStates().GetMaxFreq().IntVal))
	assert.Equal(t, "performance", profile.GetPStates().GetGovernor())
	assert.Equal(t, "performance", profile.GetPStates().GetEpp())
	assert.Nil(t, profile.GetCStates().States())
	assert.Equal(t, 10, *profile.GetCStates().GetMaxLatencyUs())

	profile, err = NewPowerProfile(
		"name", nil,
		&intstr.IntOrString{Type: intstr.Int, IntVal: 100},
		cpuPolicyPerformance, "epp", map[string]bool{}, nil,
	)
	assert.ErrorContains(t, err, fmt.Sprintf("'%s' epp can be used with '%s' governor", cpuPolicyPerformance, cpuPolicyPerformance))
	assert.Nil(t, profile)

	// Max frequency cannot be lower than the min frequency - integers
	profile, err = NewPowerProfile(
		"name",
		&intstr.IntOrString{Type: intstr.Int, IntVal: 100},
		&intstr.IntOrString{Type: intstr.Int, IntVal: 10},
		cpuPolicyPowersave,
		"epp",
		map[string]bool{},
		nil,
	)
	assert.ErrorContains(t, err, "max frequency (10) cannot be lower than the min frequency (100)")
	assert.Nil(t, profile)

	// Max frequency cannot be lower than the min frequency - percentages.
	profile, err = NewPowerProfile(
		"name",
		&intstr.IntOrString{Type: intstr.String, StrVal: "95%"},
		&intstr.IntOrString{Type: intstr.String, StrVal: "80%"},
		cpuPolicyPowersave,
		"epp",
		map[string]bool{},
		nil,
	)
	assert.ErrorContains(t, err, "max frequency (80%) cannot be lower than the min frequency (95%)")
	assert.Nil(t, profile)

	profile, err = NewPowerProfile(
		"name",
		nil,
		&intstr.IntOrString{Type: intstr.Int, IntVal: 100},
		"something random",
		"epp",
		map[string]bool{},
		nil,
	)
	assert.ErrorContains(t, err, "governor something random is not supported, please use one of the following")
	assert.Nil(t, profile)

	profile, err = NewPowerProfile(
		"name",
		nil,
		&intstr.IntOrString{Type: intstr.Int, IntVal: 100},
		cpuPolicyPowersave,
		"epp",
		map[string]bool{"C7": true},
		nil,
	)
	assert.ErrorContains(t, err, "c-state C7 does not exist on this system")
	assert.Nil(t, profile)
}

func TestAdjustMinMaxFreq(t *testing.T) {
	// Save and restore original coreTypes
	typeCopy := coreTypes
	coreTypes = CoreTypeList{&CPUFrequencySet{min: 10000, max: 1000000}} // 10MHz - 1GHz
	defer func() { coreTypes = typeCopy }()

	tests := []struct {
		name            string
		minFreq         *intstr.IntOrString
		maxFreq         *intstr.IntOrString
		expectedMinVal  string
		expectedMaxVal  string
		expectedMinType intstr.Type
		expectedMaxType intstr.Type
		expectError     bool
		errorContains   string
	}{
		{
			name:            "both nil - should return default percentages",
			minFreq:         nil,
			maxFreq:         nil,
			expectedMinVal:  "0%",
			expectedMaxVal:  "100%",
			expectedMinType: intstr.String,
			expectedMaxType: intstr.String,
			expectError:     false,
		},
		{
			name:            "minFreq nil, maxFreq int - should set min to absolute min",
			minFreq:         nil,
			maxFreq:         &intstr.IntOrString{Type: intstr.Int, IntVal: 500},
			expectedMinVal:  "10", // absoluteMinimumFrequency / 1000 = 10000 / 1000 = 10
			expectedMaxVal:  "500",
			expectedMinType: intstr.Int,
			expectedMaxType: intstr.Int,
			expectError:     false,
		},
		{
			name:            "minFreq nil, maxFreq string - should set min to 0%",
			minFreq:         nil,
			maxFreq:         &intstr.IntOrString{Type: intstr.String, StrVal: "75%"},
			expectedMinVal:  "0%",
			expectedMaxVal:  "75%",
			expectedMinType: intstr.String,
			expectedMaxType: intstr.String,
			expectError:     false,
		},
		{
			name:            "maxFreq nil, minFreq int - should set max to absolute max",
			minFreq:         &intstr.IntOrString{Type: intstr.Int, IntVal: 200},
			maxFreq:         nil,
			expectedMinVal:  "200",
			expectedMaxVal:  "1000", // absoluteMaximumFrequency / 1000 = 1000000 / 1000 = 1000
			expectedMinType: intstr.Int,
			expectedMaxType: intstr.Int,
			expectError:     false,
		},
		{
			name:            "maxFreq nil, minFreq string - should set max to 100%",
			minFreq:         &intstr.IntOrString{Type: intstr.String, StrVal: "25%"},
			maxFreq:         nil,
			expectedMinVal:  "25%",
			expectedMaxVal:  "100%",
			expectedMinType: intstr.String,
			expectedMaxType: intstr.String,
			expectError:     false,
		},
		{
			name:            "both provided with same type (int) - should return as is",
			minFreq:         &intstr.IntOrString{Type: intstr.Int, IntVal: 100},
			maxFreq:         &intstr.IntOrString{Type: intstr.Int, IntVal: 800},
			expectedMinVal:  "100",
			expectedMaxVal:  "800",
			expectedMinType: intstr.Int,
			expectedMaxType: intstr.Int,
			expectError:     false,
		},
		{
			name:            "both provided with same type (string) - should return as is",
			minFreq:         &intstr.IntOrString{Type: intstr.String, StrVal: "30%"},
			maxFreq:         &intstr.IntOrString{Type: intstr.String, StrVal: "90%"},
			expectedMinVal:  "30%",
			expectedMaxVal:  "90%",
			expectedMinType: intstr.String,
			expectedMaxType: intstr.String,
			expectError:     false,
		},
		{
			name:          "type mismatch - minFreq int, maxFreq string - should return error",
			minFreq:       &intstr.IntOrString{Type: intstr.Int, IntVal: 100},
			maxFreq:       &intstr.IntOrString{Type: intstr.String, StrVal: "90%"},
			expectError:   true,
			errorContains: "max and min frequency must be either numeric or percentage",
		},
		{
			name:          "type mismatch - minFreq string, maxFreq int - should return error",
			minFreq:       &intstr.IntOrString{Type: intstr.String, StrVal: "30%"},
			maxFreq:       &intstr.IntOrString{Type: intstr.Int, IntVal: 800},
			expectError:   true,
			errorContains: "max and min frequency must be either numeric or percentage",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			finalMinFreq, finalMaxFreq, err := AdjustMinMaxFreq(tt.minFreq, tt.maxFreq)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.ErrorContains(t, err, tt.errorContains)
				}
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.expectedMinType, finalMinFreq.Type)
			assert.Equal(t, tt.expectedMaxType, finalMaxFreq.Type)

			if finalMinFreq.Type == intstr.Int {
				assert.Equal(t, tt.expectedMinVal, fmt.Sprintf("%d", finalMinFreq.IntVal))
			} else {
				assert.Equal(t, tt.expectedMinVal, finalMinFreq.StrVal)
			}

			if finalMaxFreq.Type == intstr.Int {
				assert.Equal(t, tt.expectedMaxVal, fmt.Sprintf("%d", finalMaxFreq.IntVal))
			} else {
				assert.Equal(t, tt.expectedMaxVal, finalMaxFreq.StrVal)
			}
		})
	}
}

func TestValidatePStatesErrors(t *testing.T) {
	// Save and restore original coreTypes and availableGovs
	typeCopy := coreTypes
	coreTypes = CoreTypeList{&CPUFrequencySet{min: 100000, max: 3000000}} // 100MHz - 3GHz
	defer func() { coreTypes = typeCopy }()

	oldgovs := availableGovs
	availableGovs = []string{cpuPolicyPowersave, cpuPolicyPerformance}
	defer func() { availableGovs = oldgovs }()

	tests := []struct {
		name          string
		minFreq       intstr.IntOrString
		maxFreq       intstr.IntOrString
		governor      string
		epp           string
		expectError   bool
		errorContains string
	}{
		{
			name:          "type mismatch - minFreq int, maxFreq string",
			minFreq:       intstr.FromInt(100),
			maxFreq:       intstr.FromString("80%"),
			governor:      cpuPolicyPowersave,
			epp:           "",
			expectError:   true,
			errorContains: "max and min frequency must be either numeric or percentage",
		},
		{
			name:          "type mismatch - minFreq string, maxFreq int",
			minFreq:       intstr.FromString("20%"),
			maxFreq:       intstr.FromInt(2000),
			governor:      cpuPolicyPowersave,
			epp:           "",
			expectError:   true,
			errorContains: "max and min frequency must be either numeric or percentage",
		},
		{
			name:          "negative min frequency",
			minFreq:       intstr.FromInt(-100),
			maxFreq:       intstr.FromInt(2000),
			governor:      cpuPolicyPowersave,
			epp:           "",
			expectError:   true,
			errorContains: "min frequency must be a non-negative integer, got -100",
		},
		{
			name:          "max frequency lower than min frequency - integers",
			minFreq:       intstr.FromInt(2000),
			maxFreq:       intstr.FromInt(1000),
			governor:      cpuPolicyPowersave,
			epp:           "",
			expectError:   true,
			errorContains: "max frequency (1000) cannot be lower than the min frequency (2000)",
		},
		{
			name:          "min frequency below system minimum",
			minFreq:       intstr.FromInt(50), // Below 100MHz minimum
			maxFreq:       intstr.FromInt(2000),
			governor:      cpuPolicyPowersave,
			epp:           "",
			expectError:   true,
			errorContains: "max and min frequency must be within the range",
		},
		{
			name:          "max frequency above system maximum",
			minFreq:       intstr.FromInt(1000),
			maxFreq:       intstr.FromInt(4000), // Above 3GHz maximum
			governor:      cpuPolicyPowersave,
			epp:           "",
			expectError:   true,
			errorContains: "max and min frequency must be within the range",
		},
		{
			name:          "invalid min frequency string",
			minFreq:       intstr.FromString("invalid%"),
			maxFreq:       intstr.FromString("80%"),
			governor:      cpuPolicyPowersave,
			epp:           "",
			expectError:   true,
			errorContains: "invalid min frequency:",
		},
		{
			name:          "invalid max frequency string",
			minFreq:       intstr.FromString("20%"),
			maxFreq:       intstr.FromString("abc%"),
			governor:      cpuPolicyPowersave,
			epp:           "",
			expectError:   true,
			errorContains: "invalid max frequency:",
		},
		{
			name:          "max frequency lower than min frequency - percentages",
			minFreq:       intstr.FromString("80%"),
			maxFreq:       intstr.FromString("60%"),
			governor:      cpuPolicyPowersave,
			epp:           "",
			expectError:   true,
			errorContains: "max frequency (60%) cannot be lower than the min frequency (80%)",
		},
		{
			name:          "unsupported governor",
			minFreq:       intstr.FromInt(1000),
			maxFreq:       intstr.FromInt(2000),
			governor:      "unsupported_governor",
			epp:           "",
			expectError:   true,
			errorContains: "governor unsupported_governor is not supported, please use one of the following:",
		},
		{
			name:          "invalid epp with performance governor",
			minFreq:       intstr.FromInt(1000),
			maxFreq:       intstr.FromInt(2000),
			governor:      cpuPolicyPerformance,
			epp:           "powersave",
			expectError:   true,
			errorContains: "'performance' epp can be used with 'performance' governor",
		},
		{
			name:        "valid configuration - no error expected",
			minFreq:     intstr.FromInt(1000),
			maxFreq:     intstr.FromInt(2000),
			governor:    cpuPolicyPowersave,
			epp:         "",
			expectError: false,
		},
		{
			name:        "valid configuration with percentages - no error expected",
			minFreq:     intstr.FromString("20%"),
			maxFreq:     intstr.FromString("80%"),
			governor:    cpuPolicyPowersave,
			epp:         "",
			expectError: false,
		},
		{
			name:        "valid configuration with performance governor and epp - no error expected",
			minFreq:     intstr.FromInt(1000),
			maxFreq:     intstr.FromInt(2000),
			governor:    cpuPolicyPerformance,
			epp:         cpuPolicyPerformance,
			expectError: false,
		},
		{
			name:        "empty governor uses default - no error expected",
			minFreq:     intstr.FromInt(1000),
			maxFreq:     intstr.FromInt(2000),
			governor:    "",
			epp:         "",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePStates(tt.minFreq, tt.maxFreq, tt.governor, tt.epp)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.ErrorContains(t, err, tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
