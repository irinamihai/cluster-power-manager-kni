package power

import "fmt"

const (
	cpuTopologyDir = "topology/"
	packageIDFile  = cpuTopologyDir + "physical_package_id"
	dieIDFile      = cpuTopologyDir + "die_id"
	coreIDFile     = cpuTopologyDir + "core_id"
	clusterIDFile  = cpuTopologyDir + "cluster_id"
)

type topologyTypeObj interface {
	addCPU(uint) (CPU, error)
	CPUs() *CPUList
	getID() uint
}

// this stores the frequencies of core types
// cores can refer to this object using an array index
var coreTypes CoreTypeList

// parent struct to store system topology
type (
	cpuTopology struct {
		packages     packageList
		allCpus      CPUList
		uncore       Uncore
		architecture string
	}

	Topology interface {
		topologyTypeObj
		hasUncore
		getArchitecture() string
		Packages() *[]Package
		Package(id uint) Package
	}
)

func (s *cpuTopology) getArchitecture() string {
	return s.architecture
}

func (s *cpuTopology) addCPU(cpuID uint) (CPU, error) {
	var socketID uint
	var err error
	var cpu CPU

	if socketID, err = readCPUUintProperty(cpuID, packageIDFile); err != nil {
		return nil, err
	}
	if socket, exists := s.packages[socketID]; exists {
		cpu, err = socket.addCPU(cpuID)
	} else {
		s.packages[socketID] = &cpuPackage{
			topology: s,
			id:       socketID,
			cpus:     CPUList{},
			dies:     dieList{},
			clusters: clusterList{},
		}
		cpu, err = s.packages[socketID].addCPU(cpuID)
	}
	if err != nil {
		return nil, err
	}
	s.allCpus[cpuID] = cpu
	return cpu, err
}

func (s *cpuTopology) CPUs() *CPUList {
	return &s.allCpus
}

func (s *cpuTopology) CoreTypes() CoreTypeList {
	return coreTypes
}

func (s *cpuTopology) Packages() *[]Package {
	pkgs := make([]Package, len(s.packages))

	i := 0
	for _, pkg := range s.packages {
		pkgs[i] = pkg
		i++
	}
	return &pkgs
}

func (s *cpuTopology) Package(id uint) Package {
	pkg := s.packages[id]
	return pkg
}

func (s *cpuTopology) getID() uint {
	return 0
}

// cpu socket represents a physical cpu package
type (
	cpuPackage struct {
		topology Topology
		id       uint
		uncore   Uncore
		cpus     CPUList
		dies     dieList
		clusters clusterList
	}
	Package interface {
		hasUncore
		topologyTypeObj
		Dies() *[]Die
		Die(id uint) Die
	}
)

func (c *cpuPackage) Dies() *[]Die {
	dice := make([]Die, len(c.dies))
	i := 0
	for _, die := range c.dies {
		dice[i] = die
		i++
	}
	return &dice
}

func (c *cpuPackage) Die(id uint) Die {
	die := c.dies[id]
	return die
}

func (c *cpuPackage) addCPU(cpuID uint) (CPU, error) {
	var err error
	var cpu CPU

	architecture := c.topology.getArchitecture()
	if architecture == "" {
		return nil, fmt.Errorf("empty CPU architecture in topology")
	}
	switch architecture {
	case "x86_64":
		var dieID uint

		if dieID, err = readCPUUintProperty(cpuID, dieIDFile); err != nil {
			return nil, err
		}

		if die, exists := c.dies[dieID]; exists {
			cpu, err = die.addCPU(cpuID)
		} else {
			c.dies[dieID] = &cpuDie{
				parentSocket: c,
				id:           dieID,
				cores:        coreList{},
				cpus:         CPUList{},
			}
			cpu, err = c.dies[dieID].addCPU(cpuID)
		}
		if err != nil {
			return nil, err
		}
	case "aarch64":
		var clusterID uint
		if clusterID, err = readCPUUintProperty(cpuID, clusterIDFile); err != nil {
			return nil, err
		}

		if cluster, exists := c.clusters[clusterID]; exists {
			cpu, err = cluster.addCPU(cpuID)
		} else {
			c.clusters[clusterID] = &cpuCluster{
				parentSocket: c,
				id:           clusterID,
				cores:        coreList{},
				cpus:         CPUList{},
			}
			cpu, err = c.clusters[clusterID].addCPU(cpuID)
		}
		if err != nil {
			return nil, err
		}
	}
	c.cpus.add(cpu)
	return cpu, nil
}

func (c *cpuPackage) CPUs() *CPUList {
	return &c.cpus
}

func (c *cpuPackage) getID() uint {
	return c.id
}

type (
	cpuDie struct {
		parentSocket Package
		id           uint
		uncore       Uncore
		cores        coreList
		cpus         CPUList
	}
	Die interface {
		topologyTypeObj
		hasUncore
		Cores() *[]Core
		Core(id uint) Core
	}
)

func (d *cpuDie) Cores() *[]Core {
	cores := make([]Core, len(d.cores))
	i := 0
	for _, core := range d.cores {
		cores[i] = core
		i++
	}
	return &cores
}

func (d *cpuDie) Core(id uint) Core {
	core := d.cores[id]
	return core
}

func (d *cpuDie) CPUs() *CPUList {
	return &d.cpus
}

func (d *cpuDie) addCPU(cpuID uint) (CPU, error) {
	var err error
	var coreID uint
	var cpu CPU

	if coreID, err = readCPUUintProperty(cpuID, coreIDFile); err != nil {
		return nil, err
	}

	if core, exists := d.cores[coreID]; exists {
		cpu, err = core.addCPU(cpuID)
	} else {
		d.cores[coreID] = &cpuCore{
			parentDie: d,
			id:        coreID,
			cpus:      CPUList{},
		}
		cpu, err = d.cores[coreID].addCPU(cpuID)
	}
	if err != nil {
		return nil, err
	}
	d.cpus.add(cpu)
	return cpu, nil
}

func (d *cpuDie) getID() uint {
	return d.id
}

type (
	cpuCluster struct {
		parentSocket Package
		id           uint
		cores        coreList
		cpus         CPUList
	}
	Cluster interface {
		topologyTypeObj
		Cores() *[]Core
		Core(id uint) Core
	}
)

func (c *cpuCluster) Cores() *[]Core {
	cores := make([]Core, len(c.cores))
	i := 0
	for _, core := range c.cores {
		cores[i] = core
		i++
	}
	return &cores
}

func (c *cpuCluster) Core(id uint) Core {
	core := c.cores[id]
	return core
}

func (c *cpuCluster) CPUs() *CPUList {
	return &c.cpus
}

func (c *cpuCluster) addCPU(cpuID uint) (CPU, error) {
	var err error
	var coreID uint
	var cpu CPU

	if coreID, err = readCPUUintProperty(cpuID, coreIDFile); err != nil {
		return nil, err
	}

	if core, exists := c.cores[coreID]; exists {
		cpu, err = core.addCPU(cpuID)
	} else {
		c.cores[coreID] = &cpuCore{
			parentCluster: c,
			id:            coreID,
			cpus:          CPUList{},
		}
		cpu, err = c.cores[coreID].addCPU(cpuID)
	}
	if err != nil {
		return nil, err
	}
	c.cpus.add(cpu)
	return cpu, nil
}

func (c *cpuCluster) getID() uint {
	return c.id
}

type (
	cpuCore struct {
		parentDie     Die
		parentCluster Cluster
		id            uint
		cpus          CPUList
		// an array index pointing to a frequency set
		coreType uint
	}
	Core interface {
		topologyTypeObj
		typeSetter
	}
)

func (c *cpuCore) GetType() uint {
	return c.coreType
}

func (c *cpuCore) setType(t uint) {
	c.coreType = t
}

func (c *cpuCore) addCPU(cpuID uint) (CPU, error) {
	cpu, err := newCPU(cpuID, c)
	if err != nil {
		return nil, err
	}
	c.cpus.add(cpu)
	return cpu, nil
}

func (c *cpuCore) CPUs() *CPUList {
	return &c.cpus
}

func (c *cpuCore) getID() uint {
	return c.id
}

type packageList map[uint]Package

type dieList map[uint]Die

type clusterList map[uint]Cluster

type coreList map[uint]Core

var discoverTopology = func(arch string) (Topology, error) {
	numOfCores := getNumberOfCpus()
	topology := &cpuTopology{
		allCpus:      make(CPUList, numOfCores),
		packages:     packageList{},
		uncore:       defaultUncore,
		architecture: arch,
	}
	for i := uint(0); i < numOfCores; i++ {
		if _, err := topology.addCPU(i); err != nil {
			return nil, err
		}
	}
	return topology, nil
}
