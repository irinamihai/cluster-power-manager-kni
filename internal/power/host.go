package power

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

const (
	procCPUInfoPath = "/proc/cpuinfo"

	// unknownVendor is reported when /proc/cpuinfo is readable but carries no
	// vendor field at all. A vendor that cannot be determined is not an error;
	// only a failure to read is.
	unknownVendor = "unknown"
)

// The hostImpl is the backing object of Host interface
type hostImpl struct {
	name           string
	architecture   string
	vendorID       string
	exclusivePools PoolList
	reservedPool   Pool
	sharedPool     Pool
	topology       Topology
	featureStates  *FeatureSet

	// hostMutex serializes pool membership and power-profile updates for the host.
	// Entry points for those operations acquire it exactly once. Their internal
	// helpers assume it is already held and must not acquire it themselves. See
	// CONCURRENCY.md for the package locking rules.
	hostMutex sync.Locker
}

// Host represents the actual machine to be managed
type Host interface {
	SetName(name string)
	GetName() string
	GetFeaturesInfo() FeatureSet

	GetArchitecture() string
	GetVendorID() string

	GetReservedPool() Pool
	GetSharedPool() Pool

	AddExclusivePool(poolName string) (Pool, error)
	GetExclusivePool(poolName string) Pool
	GetAllExclusivePools() *PoolList

	GetAllCpus() *CPUList
	GetFreqRanges() CoreTypeList
	Topology() Topology
	// returns number of distinct core types
	NumCoreTypes() uint

	getHostMutex() sync.Locker
}

// lockHostMutex acquires the host mutex at the boundary of a pool mutation.
// Internal mutation helpers must not call it.
func lockHostMutex(mutex sync.Locker, keysAndValues ...interface{}) func() {
	log.V(4).Info("host mutex lock", keysAndValues...)
	mutex.Lock()
	return func() {
		mutex.Unlock()
		log.V(4).Info("host mutex unlock", keysAndValues...)
	}
}

// create a pre-populated Host object
func initHost(nodeName string) (Host, error) {

	host := &hostImpl{
		name:           nodeName,
		hostMutex:      &sync.Mutex{},
		exclusivePools: PoolList{},
	}
	host.featureStates = &featureList

	// setVendorID reads host.architecture, so this order matters.
	if err := host.setArchitecture(); err != nil {
		return nil, fmt.Errorf("failed to set host architecture: %w", err)
	}
	if err := host.setVendorID(); err != nil {
		return nil, fmt.Errorf("failed to set host vendorID: %w", err)
	}
	log.Info("discovered host", "architecture", host.architecture, "vendorID", host.vendorID)

	// create predefined pools
	host.reservedPool = &reservedPoolType{poolImpl{
		name: reservedPoolName,
		host: host,
	}}
	host.sharedPool = &sharedPoolType{poolImpl{
		name: sharedPoolName,
		cpus: CPUList{},
		host: host,
	}}

	topology, err := discoverTopology(host.architecture)
	if err != nil {
		log.Error(err, "failed to discover cpuTopology")
		return nil, fmt.Errorf("failed to init host: %w", err)
	}
	for _, cpu := range *topology.CPUs() {
		cpu._setPoolProperty(host.reservedPool)
	}

	log.Info("discovered cpus", "cpus", len(*topology.CPUs()))

	host.topology = topology

	// create a shallow copy of pointers, changes to underlying cpu object will reflect in both lists,
	// changes to each list will not affect the other
	host.reservedPool.(*reservedPoolType).cpus = make(CPUList, len(*topology.CPUs()))
	copy(host.reservedPool.(*reservedPoolType).cpus, *topology.CPUs())
	return host, nil
}

func (host *hostImpl) SetName(name string) {
	host.name = name
}

func (host *hostImpl) GetName() string {
	return host.name
}

func (host *hostImpl) GetReservedPool() Pool {
	return host.reservedPool
}

func (host *hostImpl) getHostMutex() sync.Locker {
	return host.hostMutex
}

// getHostArchitecture returns the machine architecture as the kernel reports it,
// e.g. "x86_64" or "aarch64". Defined as a var so the unit tests can pin an
// architecture that matches their sysfs fixtures; see PinTestHostIdentity.
var getHostArchitecture = func() (string, error) {
	var uts unix.Utsname
	if err := unix.Uname(&uts); err != nil {
		return "", err
	}
	return machineName(&uts), nil
}

// machineName renders the Machine field of a Utsname. The field is a
// fixed-size array the kernel leaves NUL-padded, so a plain string conversion
// would carry the padding into the value topology.go switches on.
func machineName(uts *unix.Utsname) string {
	return unix.ByteSliceToString(uts.Machine[:])
}

func (host *hostImpl) setArchitecture() error {
	arch, err := getHostArchitecture()
	if err != nil {
		return fmt.Errorf("failed to get architecture from uname: %w", err)
	}
	host.architecture = arch
	return nil
}

func (host *hostImpl) GetArchitecture() string {
	return host.architecture
}

// readCPUInfoField returns the first value for key in /proc/cpuinfo. Defined as
// a var so tests can redirect or stub host detection; see PinTestHostIdentity.
var readCPUInfoField = func(key string) (string, bool, error) {
	return readCPUInfoFieldFrom(procCPUInfoPath, key)
}

// readCPUInfoFieldFrom returns the first value for key in a cpuinfo-formatted
// file, and whether the file carried that key at all. An absent key is not an
// error: it only means the value cannot be determined. A returned error always
// means the file could not be read, which callers may treat as fatal.
func readCPUInfoFieldFrom(path, key string) (string, bool, error) {
	f, err := os.Open(path) //nolint:gosec
	if err != nil {
		return "", false, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		k, v, cut := strings.Cut(scanner.Text(), ":")
		if cut && strings.TrimSpace(k) == key {
			return strings.TrimSpace(v), true, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", false, err
	}
	return "", false, nil
}

// armImplementers maps the MIDR_EL1 implementer byte (bits 31:24) to a vendor
// name. Sourced from util-linux and verified entry-for-entry against it on
// 2026-09-28. To re-sync:
// https://github.com/util-linux/util-linux/blob/master/sys-utils/lscpu-arm.c
var armImplementers = map[uint64]string{
	0x41: "ARM", 0x42: "Broadcom", 0x43: "Cavium",
	0x44: "DEC", 0x46: "FUJITSU", 0x48: "HiSilicon",
	0x49: "Infineon", 0x4d: "Motorola/Freescale",
	0x4e: "NVIDIA", 0x50: "APM", 0x51: "Qualcomm",
	0x53: "Samsung", 0x56: "Marvell", 0x61: "Apple",
	0x66: "Faraday", 0x69: "Intel", 0x6d: "Microsoft",
	0x70: "Phytium", 0xc0: "Ampere",
}

// setVendorID reads the CPU vendor from /proc/cpuinfo, producing the same
// strings lscpu does: the raw vendor on x86, a decoded implementer name on ARM.
// It reads host.architecture, so setArchitecture must have run first.
//
// A returned error always means the host could not be read, never merely that
// the vendor is unidentifiable, so callers can treat it as fatal.
func (host *hostImpl) setVendorID() error {
	if host.architecture == "aarch64" {
		raw, found, err := readCPUInfoField("CPU implementer")
		if err != nil {
			return err
		}
		if !found {
			host.vendorID = unknownVendor
			return nil
		}
		// base 0 handles the "0x" prefix the kernel emits
		id, err := strconv.ParseUint(raw, 0, 8)
		if err != nil {
			return fmt.Errorf("malformed CPU implementer %q: %w", raw, err)
		}
		name, ok := armImplementers[id]
		if !ok {
			// a real implementer we do not have a name for, so keep the byte
			name = fmt.Sprintf("%s (0x%02x)", unknownVendor, id)
		}
		host.vendorID = name
		return nil
	}

	vendor, found, err := readCPUInfoField("vendor_id")
	if err != nil {
		return err
	}
	if !found {
		host.vendorID = unknownVendor
		return nil
	}
	host.vendorID = vendor
	return nil
}

func (host *hostImpl) GetVendorID() string {
	return host.vendorID
}

// returns default min/max frequency range
func (host *hostImpl) GetFreqRanges() CoreTypeList {
	return coreTypes
}

// AddExclusivePool creates new empty pool
func (host *hostImpl) AddExclusivePool(poolName string) (Pool, error) {
	unlock := lockHostMutex(host.hostMutex, "pool", poolName)
	defer unlock()

	if i := host.exclusivePools.IndexOfName(poolName); i >= 0 {
		return host.exclusivePools[i], fmt.Errorf("pool with name %s already exists", poolName)
	}
	var pool Pool = &exclusivePoolType{poolImpl{
		name: poolName,
		cpus: make([]CPU, 0),
		host: host,
	}}

	host.exclusivePools.add(pool)
	return pool, nil
}

// GetExclusivePool Returns a Pool object of the exclusive pool with matching name supplied
// returns nil if not found
func (host *hostImpl) GetExclusivePool(name string) Pool {
	return host.exclusivePools.ByName(name)
}

// GetSharedPool returns shared pool
func (host *hostImpl) GetSharedPool() Pool {
	return host.sharedPool
}

func (host *hostImpl) GetFeaturesInfo() FeatureSet {
	return *host.featureStates
}

func (host *hostImpl) GetAllCpus() *CPUList {
	return host.topology.CPUs()
}

func (host *hostImpl) GetAllExclusivePools() *PoolList {
	return &host.exclusivePools
}

func (host *hostImpl) NumCoreTypes() uint {
	return uint(len(coreTypes))
}

func (host *hostImpl) Topology() Topology {
	return host.topology
}
