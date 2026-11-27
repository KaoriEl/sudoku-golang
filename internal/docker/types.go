package docker

type Container struct {
	ID              string
	Name            string
	State           State
	Config          Config
	NetworkSettings NetworkSettings
}

type State struct {
	Status     string
	Running    bool
	Paused     bool
	Restarting bool
	OOMKilled  bool
	Dead       bool
	ExitCode   int
	Error      string
	StartedAt  string
	FinishedAt string
	Health     *Health
}

type Health struct {
	Status string
}

type Config struct {
	Hostname     string
	Image        string
	Labels       map[string]string
	ExposedPorts map[string]struct{}
}

type NetworkSettings struct {
	Ports    map[string][]PortBinding
	Networks map[string]EndpointSettings
}

type PortBinding struct {
	HostIP   string
	HostPort string
}

type EndpointSettings struct {
	IPAddress         string
	GlobalIPv6Address string
	Aliases           []string
	DNSNames          []string
}
