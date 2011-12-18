package directory

import (
	"gob"
	"os"
	"path"
	"rand"
	"log"
	"storage"
	"sync"
)

const (
	ChunkSizeLimit = 1 * 1024 * 1024 * 1024 //1G, can not be more than max(uint32)*8
)

type Machine struct {
	Server       string //<server name/ip>[:port]
	PublicServer string
	Volumes      []storage.VolumeInfo
	Capacity     int
}

type Mapper struct {
	dir      string
	fileName string
	capacity int

	lock          sync.Mutex
	Machines      []*Machine
	vid2machineId map[uint64]int
	writers       []int // transient array of writers volume id

	GlobalVolumeSequence uint64
}

func NewMachine(server, publicServer string, volumes []storage.VolumeInfo, capacity int) (m *Machine) {
	m = new(Machine)
	m.Server, m.PublicServer, m.Volumes, m.Capacity = server, publicServer, volumes, capacity
	return
}

func NewMapper(dirname string, filename string, capacity int) (m *Mapper) {
	m = new(Mapper)
	m.dir, m.fileName, m.capacity = dirname, filename, capacity
	log.Println("Loading volume id to maching mapping:", path.Join(m.dir, m.fileName+".map"))
	dataFile, e := os.OpenFile(path.Join(m.dir, m.fileName+".map"), os.O_RDONLY, 0644)
    m.vid2machineId = make(map[uint64]int)
    m.writers = *new([]int)
	if e != nil {
		log.Println("Mapping File Read", e)
		m.Machines = *new([]*Machine)
	} else {
		decoder := gob.NewDecoder(dataFile)
		defer dataFile.Close()
		decoder.Decode(&m.Machines)
		decoder.Decode(&m.GlobalVolumeSequence)

		//add to vid2machineId map, and writers array
		for machine_index, machine := range m.Machines {
			for _, v := range machine.Volumes {
		        m.vid2machineId[v.Id] = machine_index
				if v.Size < ChunkSizeLimit {
					m.writers = append(m.writers, machine_index)
				}
			}
		}
		log.Println("Loaded mapping size", len(m.Machines))
	}
	return
}
func (m *Mapper) PickForWrite() *Machine {
	vid := rand.Intn(len(m.writers))
	return m.Machines[m.writers[vid]]
}
func (m *Mapper) Get(vid int) *Machine {
	return m.Machines[vid]
}
func (m *Mapper) Add(machine Machine) []uint64 {
	log.Println("Adding existing", machine.Server, len(machine.Volumes), "volumes to dir", len(m.Machines))
	log.Println("Adding      new ", machine.Server, machine.Capacity-len(machine.Volumes), "volumes to dir", len(m.Machines))
	//check existing machine, linearly
	m.lock.Lock()
	foundExistingMachineId := -1
	for index, entry := range m.Machines {
		if machine.Server == entry.Server {
			foundExistingMachineId = index
			break
		}
	}
	machineId := foundExistingMachineId
	if machineId < 0 {
		machineId = len(m.Machines)
		m.Machines = append(m.Machines, &machine)
	}

	//generate new volumes
	vids := new([]uint64)
	for vid, i := m.GlobalVolumeSequence, len(machine.Volumes); i < machine.Capacity; i, vid = i+1, vid+1 {
		newVolume := *new(storage.VolumeInfo)
		newVolume.Id, newVolume.Size = vid, 0
		machine.Volumes = append(machine.Volumes, newVolume)
		m.vid2machineId[vid] = machineId
		log.Println("Adding volume", vid, "from", machine.Server)
		*vids = append(*vids, vid)
		m.GlobalVolumeSequence = vid + 1
	}

	m.Save()
	m.lock.Unlock()

	//add to vid2machineId map, and writers array
	for _, v := range machine.Volumes {
		log.Println("Setting volume", v.Id, "to", machine.Server)
		m.vid2machineId[v.Id] = machineId
		if v.Size < ChunkSizeLimit {
			m.writers = append(m.writers, machineId)
		}
	}
	//setting writers, copy-on-write because of possible updating
	var writers []int
    for machine_index, machine_entry := range m.Machines {
        for _, v := range machine_entry.Volumes {
            if v.Size < ChunkSizeLimit {
                writers = append(writers, machine_index)
            }
        }
    }
    m.writers = writers

	log.Println("Machines:", len(m.Machines), "Volumes:", len(m.vid2machineId), "Writable:", len(m.writers))
	return *vids
}
func (m *Mapper) Save() {
	log.Println("Saving virtual to physical:", path.Join(m.dir, m.fileName+".map"))
	dataFile, e := os.OpenFile(path.Join(m.dir, m.fileName+".map"), os.O_CREATE|os.O_WRONLY, 0644)
	if e != nil {
		log.Fatalf("Mapping File Save [ERROR] %s\n", e)
	}
	defer dataFile.Close()
	encoder := gob.NewEncoder(dataFile)
	encoder.Encode(m.Machines)
	encoder.Encode(m.GlobalVolumeSequence)
}
