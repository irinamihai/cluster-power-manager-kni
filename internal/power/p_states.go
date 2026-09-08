package power

// collection of Scaling Driver specific functions and methods

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	pStatesDrvFile = "cpufreq/scaling_driver"

	cpuMaxFreqFile      = "cpufreq/cpuinfo_max_freq"
	cpuMinFreqFile      = "cpufreq/cpuinfo_min_freq"
	scalingMaxFile      = "cpufreq/scaling_max_freq"
	scalingMinFile      = "cpufreq/scaling_min_freq"
	scalingSetSpeedFile = "cpufreq/scaling_setspeed"
	scalingCurFreqFile  = "cpufreq/scaling_cur_freq"

	scalingGovFile = "cpufreq/scaling_governor"
	availGovFile   = "cpufreq/scaling_available_governors"
	eppFile        = "cpufreq/energy_performance_preference"

	defaultEpp      = "power"
	defaultGovernor = cpuPolicyPowersave

	cpuPolicyPerformance  = "performance"
	cpuPolicyPowersave    = "powersave"
	cpuPolicyUserspace    = "userspace"
	cpuPolicyOndemand     = "ondemand"
	cpuPolicySchedutil    = "schedutil"
	cpuPolicyConservative = "conservative"
)

// pstatesImpl is a struct that contains the configurable parameters for the CPU P-states
type pstatesImpl struct {
	minFreq  intstr.IntOrString
	maxFreq  intstr.IntOrString
	epp      string
	governor string
}

// PStates provides access to CPU P-state configuration
type PStates interface {
	GetMinFreq() intstr.IntOrString
	GetMaxFreq() intstr.IntOrString
	GetGovernor() string
	GetEpp() string
}

func (p *pstatesImpl) GetMinFreq() intstr.IntOrString {
	return p.minFreq
}

func (p *pstatesImpl) GetMaxFreq() intstr.IntOrString {
	return p.maxFreq
}

func (p *pstatesImpl) GetGovernor() string {
	return p.governor
}

func (p *pstatesImpl) GetEpp() string {
	return p.epp
}

type (
	CPUFrequencySet struct {
		min uint
		max uint
	}
	FreqSet interface {
		GetMin() uint
		GetMax() uint
	}
	typeSetter interface {
		GetType() uint
		setType(uint)
	}
	CoreTypeList []FreqSet
)

func (s *CPUFrequencySet) GetMin() uint {
	return s.min
}

func (s *CPUFrequencySet) GetMax() uint {
	return s.max
}

// returns the index of a frequency set in a list and appends it if it's not
// in the list already. this index is used to classify a core's type
func (l *CoreTypeList) appendIfUnique(min uint, max uint) uint {
	for i, coreType := range *l {
		if coreType.GetMin() == min && coreType.GetMax() == max {
			// core type exists so return index
			return uint(i)
		}
	}
	// core type doesn't exist so append it and return index
	coreTypes = append(coreTypes, &CPUFrequencySet{min: min, max: max})
	return uint(len(coreTypes) - 1)
}

func (l *CoreTypeList) getAbsMinMaxFreq() (uint, uint) {
	min := (*l)[0].GetMin()
	max := (*l)[0].GetMax()
	for i := 1; i < len(*l); i++ {
		if (*l)[i].GetMin() < min {
			min = (*l)[i].GetMin()
		}
		if (*l)[i].GetMax() > max {
			max = (*l)[i].GetMax()
		}
	}
	return min, max
}

var allCPUDefaultPStatesInfo []pstatesImpl
var availableGovs []string

func isScalingDriverSupported(driver string) bool {
	supportedDrivers := []string{
		"intel_pstate",
		"intel_cpufreq",
		"acpi-cpufreq",
		"amd-pstate",
		"amd-pstate-epp",
		"cppc_cpufreq",
	}
	for _, s := range supportedDrivers {
		if driver == s {
			return true
		}
	}
	return false
}

func initScalingDriver() featureStatus {
	pStates := featureStatus{
		name:     "Frequency-Scaling",
		initFunc: initScalingDriver,
	}
	var err error
	availableGovs, err = initAvailableGovernors()
	if err != nil {
		pStates.err = fmt.Errorf("failed to read available governors: %w", err)
	}
	driver, err := readCPUStringProperty(0, pStatesDrvFile)
	if err != nil {
		pStates.err = fmt.Errorf("%s - failed to read driver name: %w", pStates.name, err)
	}
	pStates.driver = driver
	if !isScalingDriverSupported(driver) {
		pStates.err = fmt.Errorf("%s - unsupported driver: %s", pStates.name, driver)
	}
	if err != nil {
		pStates.err = fmt.Errorf("%s - failed to determine driver: %w", pStates.name, err)
	}
	if pStates.err == nil {
		if err := generateDefaultPStates(); err != nil {
			pStates.err = fmt.Errorf("failed to read default frequencies: %w", err)
		}
	}
	return pStates
}

func initEpp() featureStatus {
	epp := featureStatus{
		name:     "Energy-Performance-Preference",
		initFunc: initEpp,
	}
	_, err := readCPUStringProperty(0, eppFile)
	if os.IsNotExist(errors.Unwrap(err)) {
		epp.err = fmt.Errorf("EPP file %s does not exist", eppFile)
	}
	return epp
}

func initAvailableGovernors() ([]string, error) {
	govs, err := readCPUStringProperty(0, availGovFile)
	if err != nil {
		return []string{}, err
	}
	return strings.Split(govs, " "), nil
}

func GetAvailableGovernors() []string {
	return availableGovs
}

func generateDefaultPStates() error {
	numCpus := getNumberOfCpus()
	allCPUDefaultPStatesInfo = make([]pstatesImpl, numCpus)
	for cpuID := uint(0); cpuID < numCpus; cpuID++ {
		cpuInfoMaxFreq, err := readCPUUintProperty(cpuID, cpuMaxFreqFile)
		if err != nil {
			return err
		}
		cpuInfoMinFreq, err := readCPUUintProperty(cpuID, cpuMinFreqFile)
		if err != nil {
			return err
		}

		_, err = readCPUStringProperty(cpuID, eppFile)
		epp := defaultEpp
		if os.IsNotExist(errors.Unwrap(err)) {
			epp = ""
		}
		allCPUDefaultPStatesInfo[cpuID] = pstatesImpl{
			maxFreq:  intstr.FromInt(int(cpuInfoMaxFreq)),
			minFreq:  intstr.FromInt(int(cpuInfoMinFreq)),
			epp:      epp,
			governor: defaultGovernor,
		}
	}
	return nil
}

func (cpu *cpuImpl) updateFrequencies() error {
	if !IsFeatureSupported(FrequencyScalingFeature) {
		return nil
	}

	profile := cpu.pool.GetPowerProfile()
	if profile != nil {
		return cpu.setDriverValues(profile.GetPStates())
	}
	return cpu.setDriverValues(&allCPUDefaultPStatesInfo[cpu.id])
}

// setDriverValues is an entrypoint to power governor feature consolidation
func (cpu *cpuImpl) setDriverValues(pstates PStates) error {
	if err := cpu.writeGovernorValue(pstates.GetGovernor()); err != nil {
		return fmt.Errorf("failed to set governor for cpu %d: %w", cpu.id, err)
	}
	if pstates.GetEpp() != "" {
		if err := cpu.writeEppValue(pstates.GetEpp()); err != nil {
			return fmt.Errorf("failed to set EPP value for cpu %d: %w", cpu.id, err)
		}
	}
	cpuAbsMinFreq, cpuAbsMaxFreq := cpu.GetAbsMinMax()
	systemMinFreq, systemMaxFreq := coreTypes.getAbsMinMaxFreq()
	minRequestedFreq, maxRequestedFreq, err := cpu.getFreqsToScale(pstates)
	if err != nil {
		return fmt.Errorf("failed to get frequencies to scale: %w", err)
	}
	// If maxFreq and minFreq are within the system's absolute min and max, then we can set the values to
	// the hardware min and max.
	// This is meant to address ARM systems where the hardware max can be different among cores.
	if maxRequestedFreq > cpuAbsMaxFreq && maxRequestedFreq <= systemMaxFreq {
		maxRequestedFreq = cpuAbsMaxFreq
	}
	if minRequestedFreq < cpuAbsMinFreq && minRequestedFreq >= systemMinFreq {
		minRequestedFreq = cpuAbsMinFreq
	}

	if maxRequestedFreq > cpuAbsMaxFreq || minRequestedFreq < cpuAbsMinFreq {
		// If maxFreq and minFreq are not within the system's absolute min and max, then we can't set the values to
		// the hardware min and max.
		return fmt.Errorf("setting frequency %d-%d aborted as frequency range is min: %d max: %d. resetting to default",
			pstates.GetMinFreq().IntVal, pstates.GetMaxFreq().IntVal, cpuAbsMinFreq, cpuAbsMaxFreq)
	}

	if err := cpu.writeScalingMaxFreq(maxRequestedFreq); err != nil {
		return fmt.Errorf("failed to set MaxFreq value for cpu %d: %w", cpu.id, err)
	}
	if err := cpu.writeScalingMinFreq(minRequestedFreq); err != nil {
		return fmt.Errorf("failed to set MinFreq value for cpu %d: %w", cpu.id, err)
	}
	return nil
}

func (cpu *cpuImpl) getFreqsToScale(pstates PStates) (uint, uint, error) {
	// E-cores and P-cores are not currently exposed in any of the CRDs, so just return
	// the default mininum and maximum frequencies. This ensures the proper functionality
	// on both ARM and x86.
	// Update this when the operator will expose E/P cores.
	if pstates.GetMinFreq().Type != pstates.GetMaxFreq().Type {
		return 0, 0, fmt.Errorf("min and max frequencies are not of the same type")
	}

	cpuMaxFreq := allCPUDefaultPStatesInfo[cpu.id].maxFreq.IntVal
	cpuMinFreq := allCPUDefaultPStatesInfo[cpu.id].minFreq.IntVal
	requestedMinFreq := pstates.GetMinFreq()
	requestedMaxFreq := pstates.GetMaxFreq()

	minFreq, err := getFreqFromIntOrString(
		requestedMinFreq,
		uint(cpuMinFreq),
		uint(cpuMaxFreq),
	)
	if err != nil {
		return 0, 0, err
	}
	maxFreq, err := getFreqFromIntOrString(
		requestedMaxFreq,
		uint(cpuMinFreq),
		uint(cpuMaxFreq),
	)
	if err != nil {
		return 0, 0, err
	}
	return minFreq, maxFreq, nil
}

func getFreqFromIntOrString(requestedFreq intstr.IntOrString, minFreq, maxFreq uint) (uint, error) {
	if requestedFreq.Type == intstr.Int {
		return uint(requestedFreq.IntVal), nil
	}

	deltaFreq := int(maxFreq - minFreq)
	scaledDeltaFreq, err := intstr.GetScaledValueFromIntOrPercent(&requestedFreq, deltaFreq, true)
	if err != nil {
		return 0, err
	}

	return minFreq + uint(scaledDeltaFreq), nil
}

func (cpu *cpuImpl) writeGovernorValue(governor string) error {
	return os.WriteFile(filepath.Join(basePath, fmt.Sprint("cpu", cpu.id), scalingGovFile), []byte(governor), 0o644) //nolint:gosec
}

func (cpu *cpuImpl) writeEppValue(eppValue string) error {
	return os.WriteFile(filepath.Join(basePath, fmt.Sprint("cpu", cpu.id), eppFile), []byte(eppValue), 0o644) //nolint:gosec
}

func (cpu *cpuImpl) writeScalingMaxFreq(freq uint) error {
	scalingFile := filepath.Join(basePath, fmt.Sprint("cpu", cpu.id), scalingMaxFile)
	f, err := os.OpenFile( //nolint:gosec
		scalingFile,
		os.O_WRONLY|os.O_TRUNC|os.O_CREATE,
		0o644,
	)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = fmt.Fprint(f, freq)
	if err != nil {
		return err
	}
	return nil
}

func (cpu *cpuImpl) writeScalingMinFreq(freq uint) error {
	scalingFile := filepath.Join(basePath, fmt.Sprint("cpu", cpu.id), scalingMinFile)
	f, err := os.OpenFile( //nolint:gosec
		scalingFile,
		os.O_WRONLY|os.O_TRUNC|os.O_CREATE,
		0o644,
	)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = fmt.Fprint(f, freq)
	if err != nil {
		return err
	}
	return nil
}

// SetCPUFrequency sets the CPU frequency in kHz for the specified CPU using the userspace governor.
func (cpu *cpuImpl) SetCPUFrequency(frequency uint) error {
	scalingSetspeedPath := filepath.Join(basePath, fmt.Sprint("cpu", cpu.id), scalingSetSpeedFile)
	// Write the desired frequency
	err := os.WriteFile(scalingSetspeedPath, []byte(fmt.Sprintf("%d", frequency)), 0o644) //nolint:gosec
	if err != nil {
		return fmt.Errorf("failed to set frequency for CPU %d: %w", cpu.id, err)
	}

	return nil
}

// GetCurrentCPUFrequency returns the CPU frequency in kHz for the specified CPU.
func (cpu *cpuImpl) GetCurrentCPUFrequency() (uint, error) {
	freq, err := readCPUUintProperty(cpu.id, scalingCurFreqFile)
	if err != nil {
		return 0, fmt.Errorf("failed to read current frequency for CPU %d: %w", cpu.id, err)
	}
	return freq, nil
}
