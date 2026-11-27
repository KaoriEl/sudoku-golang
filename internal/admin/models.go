package admin

import (
	"sudoku-golang/internal/updates"
	"time"
)

const dockerRunning = "running"

type Status string

const (
	StatusRunning    Status = "running"
	StatusStarting   Status = "starting"
	StatusUnhealthy  Status = "unhealthy"
	StatusRestarting Status = "restarting"
	StatusPaused     Status = "paused"
	StatusStopped    Status = "stopped"
	StatusError      Status = "error"
)

type Container struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Project     string          `json:"project"`
	Service     string          `json:"service"`
	ComposeFile string          `json:"composeFile"`
	Managed     bool            `json:"managed"`
	Image       string          `json:"image"`
	State       string          `json:"state"`
	Health      string          `json:"health"`
	Status      Status          `json:"status"`
	ExitCode    int             `json:"exitCode"`
	Error       string          `json:"error"`
	StartedAt   string          `json:"startedAt"`
	FinishedAt  string          `json:"finishedAt"`
	Hostname    string          `json:"hostname"`
	Networks    []Network       `json:"networks"`
	Ports       []Port          `json:"ports"`
	Hosts       []string        `json:"hosts"`
	Routers     []Router        `json:"routers"`
	Disabled    bool            `json:"disabled"`
	Update      *updates.Status `json:"update"`
}

type Router struct {
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	EntryPoints []string `json:"entryPoints"`
	Rule        string   `json:"rule"`
}

type Network struct {
	Name     string   `json:"name"`
	IP       string   `json:"ip"`
	IPv6     string   `json:"ipv6"`
	DNSNames []string `json:"dnsNames"`
}

type Port struct {
	ContainerPort int    `json:"containerPort"`
	Protocol      string `json:"protocol"`
	HostIP        string `json:"hostIp"`
	HostPort      int    `json:"hostPort"`
}

type ComposeFile struct {
	Path     string   `json:"path"`
	Rel      string   `json:"rel"`
	Project  string   `json:"project"`
	Infra    bool     `json:"infra"`
	Services []string `json:"services"`
	Disabled []string `json:"disabled"`
	Error    string   `json:"error"`
}

type Snapshot struct {
	Root         string        `json:"root"`
	ComposeFiles []ComposeFile `json:"composeFiles"`
	Containers   []Container   `json:"containers"`
	Run          *Run          `json:"run"`
	MaxWorkers   int           `json:"maxWorkers"`
	DisabledPath string        `json:"disabledPath"`
	GeneratedAt  time.Time     `json:"generatedAt"`
	Live         bool          `json:"live"`
}

type RunStatus string

const (
	RunRunning RunStatus = "running"
	RunSuccess RunStatus = "success"
	RunPartial RunStatus = "partial"
	RunFailed  RunStatus = "failed"
)

type StepStatus string

const (
	StepPending StepStatus = "pending"
	StepRunning StepStatus = "running"
	StepSuccess StepStatus = "success"
	StepFailed  StepStatus = "failed"
	StepSkipped StepStatus = "skipped"
)

type Run struct {
	ID         string     `json:"id"`
	Action     string     `json:"action"`
	Target     string     `json:"target"`
	Status     RunStatus  `json:"status"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt"`
	Steps      []Step     `json:"steps"`
}

type Step struct {
	ComposeFile string     `json:"composeFile"`
	Rel         string     `json:"rel"`
	Phase       string     `json:"phase"`
	Command     string     `json:"command"`
	Status      StepStatus `json:"status"`
	Output      string     `json:"output"`
	Error       string     `json:"error"`
	StartedAt   *time.Time `json:"startedAt"`
	FinishedAt  *time.Time `json:"finishedAt"`
}
