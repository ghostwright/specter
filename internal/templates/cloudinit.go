package templates

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/template"
)

const cloudInitTemplate = `#cloud-config
write_files:
  - path: /home/specter/app/.env
    owner: specter:specter
    permissions: '0600'
    content: |
      NODE_ENV=production
      PORT=3100
      AGENT_NAME={{.AgentName}}
      AGENT_ROLE={{.Role}}
{{- range .SortedEnvVars}}
      {{.Key}}={{.Value}}
{{- end}}
  - path: /etc/caddy/Caddyfile
    owner: root:root
    permissions: '0644'
    content: |
      {{.AgentName}}.{{.Domain}} {
          reverse_proxy localhost:3100
      }
runcmd:
  - systemctl restart caddy
  - rm -f /var/lib/cloud/instance/user-data.txt
  - rm -f /var/lib/cloud/instance/scripts/part-001
`

type EnvVar struct {
	Key   string
	Value string
}

type CloudInitData struct {
	AgentName string
	Domain    string
	Role      string
	EnvVars   map[string]string
}

func (d CloudInitData) SortedEnvVars() []EnvVar {
	if len(d.EnvVars) == 0 {
		return nil
	}
	keys := make([]string, 0, len(d.EnvVars))
	for k := range d.EnvVars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	vars := make([]EnvVar, 0, len(keys))
	for _, k := range keys {
		vars = append(vars, EnvVar{Key: k, Value: d.EnvVars[k]})
	}
	return vars
}

func RenderCloudInit(data CloudInitData) (string, error) {
	tmpl, err := template.New("cloudinit").Parse(cloudInitTemplate)
	if err != nil {
		return "", fmt.Errorf("error parsing cloud-init template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("error rendering cloud-init template: %w", err)
	}

	return buf.String(), nil
}

func ParseEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("could not open env file: %w", err)
	}
	defer f.Close()

	vars := make(map[string]string)
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid line %d in env file: %s (expected KEY=VALUE)", lineNum, line)
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		// Strip surrounding quotes if present
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		vars[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading env file: %w", err)
	}
	return vars, nil
}
