package power

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/intstr"
)

type profileImpl struct {
	name    string
	pstates PStates
	cstates CStates
}

// Profile contains both P-states and C-states information
type Profile interface {
	Name() string
	GetPStates() PStates
	GetCStates() CStates
}

func (p *profileImpl) Name() string {
	return p.name
}

func (p *profileImpl) GetPStates() PStates {
	return p.pstates
}

func (p *profileImpl) GetCStates() CStates {
	return p.cstates
}

// NewPowerProfile creates a new power profile with both P-states and C-states configuration
// C-states can be configured either with explicit names or latency-based filtering
func NewPowerProfile(name string, minFreq, maxFreq *intstr.IntOrString, governor, epp string, cstates map[string]bool, maxLatencyUs *int) (Profile, error) {
	if !featureList.isFeatureIDSupported(FrequencyScalingFeature) {
		return nil, featureList.getFeatureIDError(FrequencyScalingFeature)
	}

	finalMinFreq, finalMaxFreq, err := AdjustMinMaxFreq(minFreq, maxFreq)
	if err != nil {
		return nil, fmt.Errorf("invalid P-states configuration: %w", err)
	}

	if err := ValidatePStates(finalMinFreq, finalMaxFreq, governor, epp); err != nil {
		return nil, fmt.Errorf("invalid P-states configuration: %w", err)
	}

	if !featureList.isFeatureIDSupported(CStatesFeature) {
		return nil, featureList.getFeatureIDError(CStatesFeature)
	}

	if err := ValidateCStates(cstates, maxLatencyUs); err != nil {
		return nil, fmt.Errorf("invalid C-states configuration: %w", err)
	}

	log.Info("creating powerProfile object", "name", name)
	if finalMinFreq.Type == intstr.Int {
		finalMinFreq = intstr.FromInt(int(finalMinFreq.IntVal * 1000))
	}
	if finalMaxFreq.Type == intstr.Int {
		finalMaxFreq = intstr.FromInt(int(finalMaxFreq.IntVal * 1000))
	}
	return &profileImpl{
		name: name,
		pstates: &pstatesImpl{
			maxFreq:  finalMaxFreq,
			minFreq:  finalMinFreq,
			epp:      epp,
			governor: governor,
		},
		cstates: cstatesImpl{states: cstates, maxLatencyUs: maxLatencyUs},
	}, nil
}

func checkGov(governor string) bool {
	for _, element := range availableGovs {
		if element == governor {
			return true
		}
	}
	return false
}

// AdjustMinMaxFreq adjusts the min and max frequency to the absolute minimum and maximum frequency of the system if needed.
func AdjustMinMaxFreq(minFreq, maxFreq *intstr.IntOrString) (intstr.IntOrString, intstr.IntOrString, error) {
	var finalMinFreq, finalMaxFreq intstr.IntOrString

	absoluteMinimumFrequency, absoluteMaximumFrequency := coreTypes.getAbsMinMaxFreq()

	switch {
	case minFreq == nil && maxFreq != nil:
		finalMaxFreq = *maxFreq
		switch maxFreq.Type {
		case intstr.Int:
			finalMinFreq = intstr.FromInt(int(absoluteMinimumFrequency / 1000))
		case intstr.String:
			finalMinFreq = intstr.FromString("0%")
		}
	case maxFreq == nil && minFreq != nil:
		finalMinFreq = *minFreq
		switch minFreq.Type {
		case intstr.Int:
			finalMaxFreq = intstr.FromInt(int(absoluteMaximumFrequency / 1000))
		case intstr.String:
			finalMaxFreq = intstr.FromString("100%")
		}
	case maxFreq == nil && minFreq == nil:
		finalMinFreq = intstr.FromString("0%")
		finalMaxFreq = intstr.FromString("100%")
	default:
		finalMinFreq = *minFreq
		finalMaxFreq = *maxFreq
	}

	if finalMinFreq.Type != finalMaxFreq.Type {
		return intstr.IntOrString{}, intstr.IntOrString{}, errors.NewServiceUnavailable("max and min frequency must be either numeric or percentage")
	}

	return finalMinFreq, finalMaxFreq, nil
}

// ValidatePStates validates a new P-states configuration
func ValidatePStates(minFreq, maxFreq intstr.IntOrString, governor, epp string) error {

	if minFreq.Type != maxFreq.Type {
		return errors.NewServiceUnavailable("max and min frequency must be either numeric or percentage")
	}

	absoluteMinimumFrequency, absoluteMaximumFrequency := coreTypes.getAbsMinMaxFreq()

	switch minFreq.Type {
	case intstr.Int:
		if minFreq.IntVal < 0 {
			return fmt.Errorf("min frequency must be a non-negative integer, got %d", minFreq.IntVal)
		}
		if maxFreq.IntVal < minFreq.IntVal {
			return fmt.Errorf("max frequency (%d) cannot be lower than the min frequency (%d)", maxFreq.IntVal, minFreq.IntVal)
		}
		// Validate the min and max frequency values against the absolute minimum and maximum frequency of the system.
		if minFreq.IntVal < int32(absoluteMinimumFrequency/1000) || maxFreq.IntVal > int32(absoluteMaximumFrequency/1000) {
			return fmt.Errorf("max and min frequency must be within the range %d-%d", absoluteMinimumFrequency, absoluteMaximumFrequency)
		}
	case intstr.String:
		// Parse the min and max frequency values from the string.
		minFreqStr := strings.TrimSuffix(minFreq.StrVal, "%")
		minFreqValInt, err := strconv.Atoi(minFreqStr)
		if err != nil {
			return fmt.Errorf("invalid min frequency: %w", err)
		}
		maxFreqStr := strings.TrimSuffix(maxFreq.StrVal, "%")
		maxFreqValInt, err := strconv.Atoi(maxFreqStr)
		if err != nil {
			return fmt.Errorf("invalid max frequency: %w", err)
		}
		// Validate the min and max frequency values against each other.
		if maxFreqValInt < minFreqValInt {
			return fmt.Errorf("max frequency (%s) cannot be lower than the min frequency (%s)", maxFreq.StrVal, minFreq.StrVal)
		}
	default:
		return errors.NewServiceUnavailable("max and min frequency must be either Int or String")
	}

	if governor == "" {
		governor = defaultGovernor
	}
	if !checkGov(governor) {
		return fmt.Errorf("governor %s is not supported, please use one of the following: %v", governor, availableGovs)
	}

	if epp != "" && governor == cpuPolicyPerformance && epp != cpuPolicyPerformance {
		return fmt.Errorf("'%s' epp can be used with '%s' governor", cpuPolicyPerformance, cpuPolicyPerformance)
	}

	return nil
}

// ValidateCStates validates a new C-states configuration
func ValidateCStates(states map[string]bool, maxLatencyUs *int) error {
	if len(states) > 0 && maxLatencyUs != nil {
		return fmt.Errorf("cannot specify both explicit C-state names and latency-based configuration")
	}

	if maxLatencyUs != nil && *maxLatencyUs < 0 {
		return fmt.Errorf("maxLatencyUs must be a non-negative integer, got %d", *maxLatencyUs)
	} else if len(states) > 0 {
		for name := range states {
			if !slices.Contains(GetAvailableCStates(), name) {
				return fmt.Errorf("c-state %s does not exist on this system", name)
			}
		}
	}

	return nil
}
