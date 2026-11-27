package composeedit

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

var (
	ErrServiceNotFound  = errors.New("service not found in compose file")
	ErrFlowStyle        = errors.New("flow-style list is not supported, convert it to a block list")
	ErrIndexOutOfRange  = errors.New("port index out of range")
	ErrEntryPointExists = errors.New("entrypoint already exists")
	ErrEntryPointAbsent = errors.New("entrypoint not found")
	ErrInvalidName      = errors.New("invalid entrypoint name")
	ErrVerifyFailed     = errors.New("edited file did not round-trip")
)

const protoTCP = "tcp"

var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

type Port struct {
	Index         int    `json:"index"`
	Raw           string `json:"raw"`
	Parsed        bool   `json:"parsed"`
	HostIP        string `json:"hostIp"`
	HostPort      int    `json:"hostPort"`
	ContainerPort int    `json:"containerPort"`
	Protocol      string `json:"protocol"`
	Comment       string `json:"comment"`
	Line          int    `json:"line"`
}

type NewPort struct {
	HostIP        string
	HostPort      int
	ContainerPort int
	Protocol      string
	Comment       string
}

type EntryPoint struct {
	Name     string `json:"name"`
	Address  string `json:"address"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	Line     int    `json:"line"`
}

func parseDoc(data []byte) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, fmt.Errorf("parse yaml: %w", errors.New("empty document"))
	}
	return doc.Content[0], nil
}

func mapGet(m *yaml.Node, key string) (*yaml.Node, *yaml.Node) {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil, nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i], m.Content[i+1]
		}
	}
	return nil, nil
}

func maxLine(n *yaml.Node) int {
	line := n.Line
	for _, c := range n.Content {
		if l := maxLine(c); l > line {
			line = l
		}
	}
	return line
}

func splitLines(data []byte) []string {
	return strings.Split(string(data), "\n")
}

func joinLines(lines []string) []byte {
	return []byte(strings.Join(lines, "\n"))
}

func leadingSpace(s string) string {
	return s[:len(s)-len(strings.TrimLeft(s, " \t"))]
}

func insertLines(lines []string, at int, add ...string) []string {
	out := make([]string, 0, len(lines)+len(add))
	out = append(out, lines[:at]...)
	out = append(out, add...)
	return append(out, lines[at:]...)
}

func removeLines(lines []string, from, to int) []string {
	out := make([]string, 0, len(lines)-(to-from))
	out = append(out, lines[:from]...)
	return append(out, lines[to:]...)
}

func serviceNode(root *yaml.Node, service string) (*yaml.Node, error) {
	_, services := mapGet(root, "services")
	_, svc := mapGet(services, service)
	if svc == nil || svc.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%w: %s", ErrServiceNotFound, service)
	}
	return svc, nil
}

func ParsePortSpec(spec string) (Port, bool) {
	p := Port{Raw: spec, Protocol: protoTCP}
	s := strings.TrimSpace(spec)
	if base, proto, ok := strings.Cut(s, "/"); ok {
		s, p.Protocol = base, proto
	}

	idx := strings.LastIndex(s, ":")
	if idx < 0 {
		cp, err := strconv.Atoi(s)
		if err != nil {
			return p, false
		}
		p.ContainerPort = cp
		return p, true
	}
	cp, err := strconv.Atoi(s[idx+1:])
	if err != nil {
		return p, false
	}
	p.ContainerPort = cp
	rest := s[:idx]

	hostPart := rest
	if j := strings.LastIndex(rest, ":"); j >= 0 {
		p.HostIP = strings.Trim(rest[:j], "[]")
		hostPart = rest[j+1:]
	}
	hp, err := strconv.Atoi(hostPart)
	if err != nil {
		return p, false
	}
	p.HostPort = hp
	return p, true
}

func FormatPortSpec(p NewPort) string {
	proto := p.Protocol
	if proto == "" {
		proto = protoTCP
	}
	spec := fmt.Sprintf("%d:%d/%s", p.HostPort, p.ContainerPort, proto)
	if p.HostIP != "" {
		ip := p.HostIP
		if strings.Contains(ip, ":") {
			ip = "[" + ip + "]"
		}
		spec = ip + ":" + spec
	}
	return spec
}

func ReadPorts(data []byte, service string) ([]Port, error) {
	root, err := parseDoc(data)
	if err != nil {
		return nil, err
	}
	svc, err := serviceNode(root, service)
	if err != nil {
		return nil, err
	}
	_, seq := mapGet(svc, "ports")
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return []Port{}, nil
	}

	ports := make([]Port, 0, len(seq.Content))
	for i, item := range seq.Content {
		var p Port
		if item.Kind == yaml.ScalarNode {
			p, p.Parsed = ParsePortSpec(item.Value)
		} else {
			p = Port{Raw: fmt.Sprintf("(long syntax, line %d)", item.Line)}
		}
		p.Index = i
		p.Line = item.Line
		p.Comment = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(item.LineComment), "#"))
		ports = append(ports, p)
	}
	return ports, nil
}

// cleanComment makes a user comment safe to put after "#" on one line: every control character and
// every character YAML treats as a line break (NEL, LS, PS) becomes a space, otherwise the rest of
// the comment would turn into YAML content.
func cleanComment(c string) string {
	c = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '\u0085' || r == '\u2028' || r == '\u2029' || r == '\ufeff' {
			return ' '
		}
		return r
	}, c)
	const maxComment = 200
	if len([]rune(c)) > maxComment {
		c = string([]rune(c)[:maxComment])
	}
	return strings.TrimSpace(c)
}

func AddPort(data []byte, service string, p NewPort) ([]byte, error) {
	root, err := parseDoc(data)
	if err != nil {
		return nil, err
	}
	svc, err := serviceNode(root, service)
	if err != nil {
		return nil, err
	}
	lines := splitLines(data)
	spec := FormatPortSpec(p)
	comment := ""
	if c := cleanComment(p.Comment); c != "" {
		comment = "  # " + c
	}

	_, seq := mapGet(svc, "ports")
	switch {
	case seq != nil && seq.Kind == yaml.SequenceNode && seq.Style&yaml.FlowStyle != 0:
		return nil, ErrFlowStyle
	case seq != nil && seq.Kind == yaml.SequenceNode && len(seq.Content) > 0:
		last := seq.Content[len(seq.Content)-1]
		lastLine := lines[last.Line-1]
		quote := `"`
		if last.Style&yaml.SingleQuotedStyle != 0 {
			quote = "'"
		} else if last.Style&yaml.DoubleQuotedStyle == 0 {
			quote = ""
		}
		entry := leadingSpace(lastLine) + "- " + quote + spec + quote + comment
		lines = insertLines(lines, maxLine(last), entry)
	default:
		if len(svc.Content) == 0 {
			return nil, fmt.Errorf("%w: %s", ErrServiceNotFound, service)
		}
		firstKey := svc.Content[0]
		indent := strings.Repeat(" ", firstKey.Column-1)
		keyNode, _ := mapGet(svc, "ports")
		at := firstKey.Line - 1
		add := []string{indent + "ports:", indent + "  - \"" + spec + "\"" + comment}
		if keyNode != nil {
			lines = removeLines(lines, keyNode.Line-1, keyNode.Line)
			at = keyNode.Line - 1
		}
		lines = insertLines(lines, at, add...)
	}

	out := joinLines(lines)
	before := 0
	if seq != nil {
		before = len(seq.Content)
	}
	if err := verifyPortCount(out, service, before+1); err != nil {
		return nil, err
	}
	return out, nil
}

func RemovePort(data []byte, service string, index int, expectRaw string) ([]byte, error) {
	root, err := parseDoc(data)
	if err != nil {
		return nil, err
	}
	svc, err := serviceNode(root, service)
	if err != nil {
		return nil, err
	}
	keyNode, seq := mapGet(svc, "ports")
	if seq == nil || seq.Kind != yaml.SequenceNode || index < 0 || index >= len(seq.Content) {
		return nil, ErrIndexOutOfRange
	}
	if seq.Style&yaml.FlowStyle != 0 {
		return nil, ErrFlowStyle
	}
	item := seq.Content[index]
	if expectRaw != "" && item.Value != expectRaw {
		return nil, fmt.Errorf("%w: expected %q at index %d, found %q", ErrIndexOutOfRange, expectRaw, index, item.Value)
	}

	lines := splitLines(data)
	if len(seq.Content) == 1 {
		lines = removeLines(lines, keyNode.Line-1, maxLine(item))
	} else {
		lines = removeLines(lines, item.Line-1, maxLine(item))
	}

	out := joinLines(lines)
	if err := verifyPortCount(out, service, len(seq.Content)-1); err != nil {
		return nil, err
	}
	return out, nil
}

func verifyPortCount(data []byte, service string, want int) error {
	ports, err := ReadPorts(data, service)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrVerifyFailed, err)
	}
	if len(ports) != want {
		return fmt.Errorf("%w: expected %d ports, got %d", ErrVerifyFailed, want, len(ports))
	}
	return nil
}

func ServiceVolumeSource(data []byte, service, target string) (string, bool) {
	root, err := parseDoc(data)
	if err != nil {
		return "", false
	}
	svc, err := serviceNode(root, service)
	if err != nil {
		return "", false
	}
	_, vols := mapGet(svc, "volumes")
	if vols == nil {
		return "", false
	}
	for _, v := range vols.Content {
		if v.Kind != yaml.ScalarNode {
			continue
		}
		parts := strings.Split(v.Value, ":")
		if len(parts) >= 2 && parts[1] == target {
			return parts[0], true
		}
	}
	return "", false
}

func ServiceNames(data []byte) ([]string, error) {
	root, err := parseDoc(data)
	if err != nil {
		return nil, err
	}
	_, services := mapGet(root, "services")
	if services == nil {
		return []string{}, nil
	}
	names := make([]string, 0, len(services.Content)/2)
	for i := 0; i+1 < len(services.Content); i += 2 {
		names = append(names, services.Content[i].Value)
	}
	return names, nil
}

func ServiceImage(data []byte, service string) string {
	root, err := parseDoc(data)
	if err != nil {
		return ""
	}
	svc, err := serviceNode(root, service)
	if err != nil {
		return ""
	}
	_, img := mapGet(svc, "image")
	if img == nil {
		return ""
	}
	return img.Value
}

func parseAddress(addr string) (int, string) {
	s := addr
	proto := protoTCP
	if base, pr, ok := strings.Cut(s, "/"); ok {
		s, proto = base, pr
	}
	idx := strings.LastIndex(s, ":")
	if idx < 0 {
		return 0, proto
	}
	port, err := strconv.Atoi(s[idx+1:])
	if err != nil {
		return 0, proto
	}
	return port, proto
}

func ReadEntryPoints(data []byte) ([]EntryPoint, error) {
	root, err := parseDoc(data)
	if err != nil {
		return nil, err
	}
	_, eps := mapGet(root, "entryPoints")
	if eps == nil || eps.Kind != yaml.MappingNode {
		return []EntryPoint{}, nil
	}
	out := make([]EntryPoint, 0, len(eps.Content)/2)
	for i := 0; i+1 < len(eps.Content); i += 2 {
		ep := EntryPoint{Name: eps.Content[i].Value, Line: eps.Content[i].Line, Protocol: protoTCP}
		if _, addr := mapGet(eps.Content[i+1], "address"); addr != nil {
			ep.Address = addr.Value
			ep.Port, ep.Protocol = parseAddress(addr.Value)
		}
		out = append(out, ep)
	}
	return out, nil
}

func AddEntryPoint(data []byte, name string, port int, protocol string) ([]byte, error) {
	if !nameRe.MatchString(name) {
		return nil, fmt.Errorf("%w: %q", ErrInvalidName, name)
	}
	if protocol == "" {
		protocol = protoTCP
	}
	root, err := parseDoc(data)
	if err != nil {
		return nil, err
	}
	keyNode, eps := mapGet(root, "entryPoints")
	if _, exists := mapGet(eps, name); exists != nil {
		return nil, fmt.Errorf("%w: %s", ErrEntryPointExists, name)
	}

	lines := splitLines(data)
	address := fmt.Sprintf(`"%s"`, fmt.Sprintf(":%d/%s", port, protocol))

	switch {
	case eps != nil && eps.Kind == yaml.MappingNode && len(eps.Content) >= 2:
		if eps.Style&yaml.FlowStyle != 0 {
			return nil, ErrFlowStyle
		}
		firstKey := eps.Content[0]
		keyIndent := strings.Repeat(" ", firstKey.Column-1)
		valIndent := keyIndent + "  "
		if v := eps.Content[1]; v.Kind == yaml.MappingNode && len(v.Content) > 0 {
			valIndent = strings.Repeat(" ", v.Content[0].Column-1)
		}
		last := eps.Content[len(eps.Content)-1]
		lines = insertLines(lines, maxLine(last), "", keyIndent+name+":", valIndent+"address: "+address)
	case keyNode != nil:
		lines = removeLines(lines, keyNode.Line-1, keyNode.Line)
		lines = insertLines(lines, keyNode.Line-1, "entryPoints:", "  "+name+":", "    address: "+address)
	default:
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		lines = append(lines, "", "entryPoints:", "  "+name+":", "    address: "+address, "")
	}

	out := joinLines(lines)
	got, err := ReadEntryPoints(out)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrVerifyFailed, err)
	}
	for _, ep := range got {
		if ep.Name == name && ep.Port == port {
			return out, nil
		}
	}
	return nil, ErrVerifyFailed
}

func RemoveEntryPoint(data []byte, name string) ([]byte, error) {
	root, err := parseDoc(data)
	if err != nil {
		return nil, err
	}
	_, eps := mapGet(root, "entryPoints")
	key, val := mapGet(eps, name)
	if key == nil {
		return nil, fmt.Errorf("%w: %s", ErrEntryPointAbsent, name)
	}
	lines := splitLines(data)
	from := key.Line - 1
	if from > 0 && strings.TrimSpace(lines[from-1]) == "" {
		from--
	}
	lines = removeLines(lines, from, maxLine(val))

	out := joinLines(lines)
	got, err := ReadEntryPoints(out)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrVerifyFailed, err)
	}
	for _, ep := range got {
		if ep.Name == name {
			return nil, ErrVerifyFailed
		}
	}
	return out, nil
}
