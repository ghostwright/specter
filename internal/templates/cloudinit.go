package templates

import (
	"bytes"
	"fmt"
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
  - path: /etc/caddy/Caddyfile
    owner: root:root
    permissions: '0644'
    content: |
      {{.AgentName}}.{{.Domain}} {
          reverse_proxy localhost:3100
      }
runcmd:
  - systemctl restart caddy
`

type CloudInitData struct {
	AgentName string
	Domain    string
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
