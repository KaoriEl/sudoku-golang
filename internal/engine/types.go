package engine

import "time"

type rawInfo struct {
	NCPU              int
	MemTotal          uint64
	ServerVersion     string
	OperatingSystem   string
	KernelVersion     string
	Driver            string
	CgroupVersion     string
	Containers        int
	ContainersRunning int
	Images            int
	Name              string
}

type ListedContainer struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	Labels map[string]string `json:"Labels"`
}

// ContainerSummary is the part of a /containers/json entry used to decide whether a cached inspect
// result is still current: State and Status ("Up 5 minutes (healthy)") change on every lifecycle or
// health transition.
type ContainerSummary struct {
	ID     string `json:"Id"`
	State  string `json:"State"`
	Status string `json:"Status"`
}

type Stats struct {
	Read     time.Time `json:"read"`
	CPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemUsage uint64 `json:"system_cpu_usage"`
		OnlineCPUs  int    `json:"online_cpus"`
	} `json:"cpu_stats"`
	MemoryStats struct {
		Usage uint64   `json:"usage"`
		Limit uint64   `json:"limit"`
		Stats MemStats `json:"stats"`
	} `json:"memory_stats"`
	PidsStats struct {
		Current uint64 `json:"current"`
	} `json:"pids_stats"`
	Networks map[string]struct {
		RxBytes uint64 `json:"rx_bytes"`
		TxBytes uint64 `json:"tx_bytes"`
	} `json:"networks"`
	BlkioStats struct {
		IOServiceBytes []struct {
			Op    string `json:"op"`
			Value uint64 `json:"value"`
		} `json:"io_service_bytes_recursive"`
	} `json:"blkio_stats"`
}

type rawDF struct {
	Images []struct {
		Size       int64 `json:"Size"`
		SharedSize int64 `json:"SharedSize"`
		Containers int64 `json:"Containers"`
	} `json:"Images"`
	Containers []struct {
		SizeRw int64 `json:"SizeRw"`
	} `json:"Containers"`
	Volumes []struct {
		UsageData struct {
			Size int64 `json:"Size"`
		} `json:"UsageData"`
	} `json:"Volumes"`
	BuildCache []struct {
		Size  int64 `json:"Size"`
		InUse bool  `json:"InUse"`
	} `json:"BuildCache"`
	LayersSize int64 `json:"LayersSize"`
}

// DF is the detailed /system/df payload used by cleanup. Field names follow the Docker API.
type DF struct {
	Images     []DFImage
	Containers []DFContainer
	Volumes    []DFVolume
	BuildCache []DFBuildCache
}

type DFImage struct {
	ID         string
	RepoTags   []string
	Created    int64
	Size       int64
	SharedSize int64
	Containers int64
	Labels     map[string]string
}

type DFMount struct {
	Type        string
	Name        string
	Source      string
	Destination string
}

type DFContainer struct {
	ID      string
	Names   []string
	Image   string
	ImageID string
	State   string
	Status  string
	Created int64
	SizeRw  int64
	Labels  map[string]string
	Mounts  []DFMount
}

type DFVolume struct {
	Name      string
	Labels    map[string]string
	UsageData struct {
		Size     int64
		RefCount int64
	}
}

type DFBuildCache struct {
	ID          string
	Type        string
	Description string
	InUse       bool
	Shared      bool
	Size        int64
	LastUsedAt  *time.Time
	CreatedAt   time.Time
}

type buildPruneReport struct {
	CachesDeleted  []string
	SpaceReclaimed uint64
}

// MemStats are the only memory_stats.stats keys the collector reads. A struct instead of
// map[string]uint64 avoids allocating a ~30-entry map per container on every poll.
type MemStats struct {
	InactiveFile      *uint64 `json:"inactive_file"`
	TotalInactiveFile *uint64 `json:"total_inactive_file"`
	Cache             *uint64 `json:"cache"`
}

// Image is the part of /images/{name}/json used for update checks and backups.
type Image struct {
	ID          string   `json:"Id"`
	RepoTags    []string `json:"RepoTags"`
	RepoDigests []string `json:"RepoDigests"`
	Created     string   `json:"Created"`
}
