package templates

import "fmt"

func RenderCaddyfile(agentName, domain string) string {
	return fmt.Sprintf(`%s.%s {
    reverse_proxy localhost:3100
}
`, agentName, domain)
}
