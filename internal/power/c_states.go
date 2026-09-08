package power

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	cStatesDir                  = "cpuidle"
	cStateDisableFileFmt        = cStatesDir + "/state%d/disable"
	cStateNameFileFmt           = cStatesDir + "/state%d/name"
	cStatesDefaultStatusFileFmt = cStatesDir + "/state%d/default_status"
	cStateLatencyFileFmt        = cStatesDir + "/state%d/latency"
	cStatesDrvPath              = cStatesDir + "/current_driver"
)

type cstatesImpl struct {
	states       map[string]bool // c-state name -> enable/disable status
	maxLatencyUs *int            // maximum latency in microseconds
}

// CStates provides access to CPU C-state configuration
type CStates interface {
	States() map[string]bool
	GetMaxLatencyUs() *int
}

func (c cstatesImpl) States() map[string]bool {
	return c.states
}

func (c cstatesImpl) GetMaxLatencyUs() *int {
	return c.maxLatencyUs
}

// cstateInfo holds information about a c-state including its latency and default status in sysfs
type cstateInfo struct {
	StateNumber   int
	Latency       int  // latency in microseconds
	DefaultStatus bool // default enable/disable status
}

type cpuCStatesInfo = map[string]cstateInfo // c-state name -> c-state info

func isSupportedCStatesDriver(driver string) bool {
	for _, s := range []string{"intel_idle", "acpi_idle"} {
		if driver == s {
			return true
		}
	}
	return false
}

// per-CPU c-state information mapping
// populated during library initialisation
// organized as cpuID -> cstate name -> cstate info
var allCPUCStatesInfo = map[uint]cpuCStatesInfo{}

func initCStates() featureStatus {
	feature := featureStatus{
		name:     "C-States",
		initFunc: initCStates,
	}
	driver, err := readStringFromFile(filepath.Join(basePath, cStatesDrvPath))
	driver = strings.TrimSuffix(driver, "\n")
	feature.driver = driver
	if err != nil {
		feature.err = fmt.Errorf("failed to determine driver: %w", err)
		return feature
	}
	if !isSupportedCStatesDriver(driver) {
		feature.err = fmt.Errorf("unsupported driver: %s", driver)
		return feature
	}
	feature.err = mapAvailableCStates()

	return feature
}

// Set allCPUCStatesInfo
// Read latency and default enable/disable status for each c-state of each CPU from sysfs
func mapAvailableCStates() error {
	cStateDirNameRegex := regexp.MustCompile(`state(\d+)`)

	// Initialize per-CPU c-state information for all available CPUs
	numCpus := getNumberOfCpus()
	for cpuID := uint(0); cpuID < numCpus; cpuID++ {
		allCPUCStatesInfo[cpuID] = make(map[string]cstateInfo)

		// Read per-CPU C-state information
		cpuDirs, err := os.ReadDir(filepath.Join(basePath, fmt.Sprintf("cpu%d", cpuID), cStatesDir))
		if err != nil {
			return fmt.Errorf("could not open cpu%d C-States directory: %w", cpuID, err)
		}

		for _, stateDir := range cpuDirs {
			dirName := stateDir.Name()
			if !stateDir.IsDir() || !cStateDirNameRegex.MatchString(dirName) {
				log.Info("map C-States ignoring " + dirName)
				continue
			}
			stateNumber, err := strconv.Atoi(cStateDirNameRegex.FindStringSubmatch(dirName)[1])
			if err != nil {
				return fmt.Errorf("failed to extract cpu%d C-State number %s: %w", cpuID, dirName, err)
			}

			// Read c-state name from sysfs
			stateName, err := readCPUStringProperty(cpuID, fmt.Sprintf(cStateNameFileFmt, stateNumber))
			if err != nil {
				return fmt.Errorf("could not read cpu%d C-State %d name: %w", cpuID, stateNumber, err)
			}

			// Read c-state latency from sysfs
			latency, err := readCPUUintProperty(cpuID, fmt.Sprintf(cStateLatencyFileFmt, stateNumber))
			if err != nil {
				return fmt.Errorf("could not read cpu%d C-State %d latency: %w", cpuID, stateNumber, err)
			}

			// Get default c-state status from default_status sysfs file if it exists, otherwise set to true
			defaultStatus := true
			defaultStatusStr, err := readCPUStringProperty(cpuID, fmt.Sprintf(cStatesDefaultStatusFileFmt, stateNumber))
			if err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("could not read cpu%d C-State %d default status file: %w", cpuID, stateNumber, err)
			} else if err == nil {
				defaultStatus = defaultStatusStr == "enabled"
			}

			allCPUCStatesInfo[cpuID][stateName] = cstateInfo{
				StateNumber:   stateNumber,
				Latency:       int(latency),
				DefaultStatus: defaultStatus,
			}
		}
		log.V(4).Info("mapped C-states", "cpuID", cpuID, "map", allCPUCStatesInfo[cpuID])
	}
	return nil
}

func (cpu *cpuImpl) getDefaultCStatesStatus() map[string]bool {
	defaults := make(map[string]bool)
	for stateName, info := range allCPUCStatesInfo[cpu.id] {
		defaults[stateName] = info.DefaultStatus
	}
	return defaults
}

func GetAvailableCStates() []string {
	cStatesSet := make(map[string]bool)
	for _, cstatesInfo := range allCPUCStatesInfo {
		for stateName := range cstatesInfo {
			cStatesSet[stateName] = true
		}
	}
	cStatesList := make([]string, 0, len(cStatesSet))
	for stateName := range cStatesSet {
		cStatesList = append(cStatesList, stateName)
	}
	return cStatesList
}

// configCStatesByLatency configures C-states based on maximum latency threshold
func (cpu *cpuImpl) configCStatesByLatency(maxLatencyUs int) CStates {
	cstatesInfo := allCPUCStatesInfo[cpu.id]

	desiredCStates := make(map[string]bool)
	for stateName, stateInfo := range cstatesInfo {
		if stateInfo.Latency <= maxLatencyUs {
			// Enable C-states with latency <= maxLatencyUs
			desiredCStates[stateName] = true
		} else {
			// Disable C-states with latency > maxLatencyUs
			desiredCStates[stateName] = false
		}
	}

	log.V(4).Info("config C-states by latency", "cpuID", cpu.id, "maxLatencyUs", maxLatencyUs, "desiredCStates", desiredCStates)
	return cstatesImpl{states: desiredCStates}
}

// configCStatesByNames configures C-states based on names
func (cpu *cpuImpl) configCStatesByNames(names map[string]bool) CStates {
	cstatesInfo := allCPUCStatesInfo[cpu.id]

	desiredCStates := make(map[string]bool)
	for stateName, info := range cstatesInfo {
		if providedStatus, exists := names[stateName]; exists {
			desiredCStates[stateName] = providedStatus
		} else {
			desiredCStates[stateName] = info.DefaultStatus
		}
	}

	return cstatesImpl{states: desiredCStates}
}

func (cpu *cpuImpl) updateCStates() error {
	if !IsFeatureSupported(CStatesFeature) {
		return nil
	}

	// Get cstates config from profile
	profile := cpu.pool.GetPowerProfile()
	if profile != nil {
		if maxLatencyUs := profile.GetCStates().GetMaxLatencyUs(); maxLatencyUs != nil {
			return cpu.applyCStates(cpu.configCStatesByLatency(*maxLatencyUs))
		}
		if providedCStates := profile.GetCStates().States(); len(providedCStates) > 0 {
			return cpu.applyCStates(cpu.configCStatesByNames(providedCStates))
		}
	}

	return cpu.applyCStates(cstatesImpl{states: cpu.getDefaultCStatesStatus()})
}

func (cpu *cpuImpl) applyCStates(desiredCStates CStates) error {
	cstatesInfo := allCPUCStatesInfo[cpu.id]
	for stateName, enabled := range desiredCStates.States() {
		if _, exists := cstatesInfo[stateName]; !exists {
			log.Error(fmt.Errorf("c-state %s does not exist for cpu %d", stateName, cpu.id), "c-state does not exist")
			continue
		}

		stateFilePath := filepath.Join(
			basePath,
			fmt.Sprint("cpu", cpu.id),
			fmt.Sprintf(cStateDisableFileFmt, cstatesInfo[stateName].StateNumber),
		)
		content := make([]byte, 1)
		if enabled {
			content[0] = '0' // write '0' to enable the c state
		} else {
			content[0] = '1' // write '1' to disable the c state
		}
		if err := os.WriteFile(stateFilePath, content, 0o644); err != nil { //nolint:gosec
			return fmt.Errorf("could not apply cstate %s on cpu %d: %w", stateName, cpu.id, err)
		}
	}
	return nil
}
