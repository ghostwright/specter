package hetzner

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ghostwright/specter/internal/config"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

type Client struct {
	API *hcloud.Client
}

func NewClient(token string) *Client {
	return &Client{
		API: hcloud.NewClient(hcloud.WithToken(token)),
	}
}

func (c *Client) ValidateToken(ctx context.Context) error {
	_, err := c.API.Server.All(ctx)
	if err != nil {
		return fmt.Errorf("invalid Hetzner token: %w", err)
	}
	return nil
}

func (c *Client) GetSSHKey(ctx context.Context, name string) (*hcloud.SSHKey, error) {
	key, _, err := c.API.SSHKey.GetByName(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("error looking up SSH key: %w", err)
	}
	if key == nil {
		return nil, fmt.Errorf("SSH key '%s' not found on Hetzner. Upload it at console.hetzner.cloud", name)
	}
	return key, nil
}

func (c *Client) ListSSHKeys(ctx context.Context) ([]*hcloud.SSHKey, error) {
	keys, err := c.API.SSHKey.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("error listing SSH keys: %w", err)
	}
	return keys, nil
}

func (c *Client) CreateServer(ctx context.Context, opts hcloud.ServerCreateOpts) (*hcloud.ServerCreateResult, error) {
	result, _, err := c.API.Server.Create(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("error creating server: %w", err)
	}
	return &result, nil
}

func (c *Client) WaitForRunning(ctx context.Context, serverID int64) (*hcloud.Server, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		s, _, err := c.API.Server.GetByID(ctx, serverID)
		if err != nil {
			return nil, fmt.Errorf("error polling server: %w", err)
		}
		if s.Status == hcloud.ServerStatusRunning {
			return s, nil
		}
		time.Sleep(5 * time.Second)
	}
}

func (c *Client) DeleteServer(ctx context.Context, server *hcloud.Server) error {
	_, _, err := c.API.Server.DeleteWithResult(ctx, server)
	if err != nil {
		return fmt.Errorf("error deleting server: %w", err)
	}
	return nil
}

// IsNotFound checks if an error from hcloud-go is a 404 not found error.
func IsNotFound(err error) bool {
	return hcloud.IsError(err, hcloud.ErrorCodeNotFound)
}

func (c *Client) ListSpecterServers(ctx context.Context) ([]*hcloud.Server, error) {
	servers, err := c.API.Server.AllWithOpts(ctx, hcloud.ServerListOpts{
		ListOpts: hcloud.ListOpts{
			LabelSelector: "managed_by=specter",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("error listing servers: %w", err)
	}
	return servers, nil
}

func (c *Client) GetServerByName(ctx context.Context, name string) (*hcloud.Server, error) {
	servers, err := c.API.Server.AllWithOpts(ctx, hcloud.ServerListOpts{
		ListOpts: hcloud.ListOpts{
			LabelSelector: fmt.Sprintf("managed_by=specter,agent_name=%s", name),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("error looking up server: %w", err)
	}
	if len(servers) == 0 {
		return nil, nil
	}
	return servers[0], nil
}

func mustParseCIDR(s string) net.IPNet {
	_, ipNet, err := net.ParseCIDR(s)
	if err != nil {
		panic(fmt.Sprintf("invalid CIDR: %s", s))
	}
	return *ipNet
}

func (c *Client) CreateFirewall(ctx context.Context, name string) (*hcloud.Firewall, error) {
	fw, _, err := c.API.Firewall.GetByName(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("error checking firewall: %w", err)
	}
	if fw != nil {
		return fw, nil
	}

	allIPv4 := mustParseCIDR("0.0.0.0/0")
	allIPv6 := mustParseCIDR("::/0")

	result, _, err := c.API.Firewall.Create(ctx, hcloud.FirewallCreateOpts{
		Name: name,
		Rules: []hcloud.FirewallRule{
			{
				Direction:   hcloud.FirewallRuleDirectionIn,
				Protocol:    hcloud.FirewallRuleProtocolTCP,
				Port:        hcloud.Ptr("22"),
				SourceIPs:   []net.IPNet{allIPv4, allIPv6},
				Description: hcloud.Ptr("SSH"),
			},
			{
				Direction:   hcloud.FirewallRuleDirectionIn,
				Protocol:    hcloud.FirewallRuleProtocolTCP,
				Port:        hcloud.Ptr("80"),
				SourceIPs:   []net.IPNet{allIPv4, allIPv6},
				Description: hcloud.Ptr("HTTP"),
			},
			{
				Direction:   hcloud.FirewallRuleDirectionIn,
				Protocol:    hcloud.FirewallRuleProtocolTCP,
				Port:        hcloud.Ptr("443"),
				SourceIPs:   []net.IPNet{allIPv4, allIPv6},
				Description: hcloud.Ptr("HTTPS"),
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("error creating firewall: %w", err)
	}

	return result.Firewall, nil
}

func (c *Client) ShutdownServer(ctx context.Context, server *hcloud.Server) error {
	action, _, err := c.API.Server.Shutdown(ctx, server)
	if err != nil {
		return fmt.Errorf("error shutting down server: %w", err)
	}
	_, errCh := c.API.Action.WatchProgress(ctx, action)
	if err := <-errCh; err != nil {
		return fmt.Errorf("error waiting for shutdown: %w", err)
	}
	return nil
}

func (c *Client) PowerOffServer(ctx context.Context, server *hcloud.Server) error {
	action, _, err := c.API.Server.Poweroff(ctx, server)
	if err != nil {
		return fmt.Errorf("error powering off server: %w", err)
	}
	_, errCh := c.API.Action.WatchProgress(ctx, action)
	if err := <-errCh; err != nil {
		return fmt.Errorf("error waiting for power off: %w", err)
	}
	return nil
}

func (c *Client) CreateSnapshot(ctx context.Context, server *hcloud.Server, description string, labels map[string]string) (*hcloud.Image, error) {
	result, _, err := c.API.Server.CreateImage(ctx, server, &hcloud.ServerCreateImageOpts{
		Type:        hcloud.ImageTypeSnapshot,
		Description: hcloud.Ptr(description),
		Labels:      labels,
	})
	if err != nil {
		return nil, fmt.Errorf("error creating snapshot: %w", err)
	}

	_, errCh := c.API.Action.WatchProgress(ctx, result.Action)
	if err := <-errCh; err != nil {
		return nil, fmt.Errorf("error waiting for snapshot: %w", err)
	}

	// Re-fetch to get populated DiskSize (0 in initial response)
	updated, _, err := c.API.Image.GetByID(ctx, result.Image.ID)
	if err == nil && updated != nil {
		return updated, nil
	}
	return result.Image, nil
}

func (c *Client) FindSpecterSnapshot(ctx context.Context) (*hcloud.Image, error) {
	images, err := c.API.Image.AllWithOpts(ctx, hcloud.ImageListOpts{
		Type: []hcloud.ImageType{hcloud.ImageTypeSnapshot},
		ListOpts: hcloud.ListOpts{
			LabelSelector: "managed_by=specter",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("error listing snapshots: %w", err)
	}
	if len(images) == 0 {
		return nil, nil
	}

	var latest *hcloud.Image
	for _, img := range images {
		if latest == nil || img.Created.After(latest.Created) {
			latest = img
		}
	}
	return latest, nil
}

func (c *Client) ListServerTypes(ctx context.Context) ([]config.ServerTypeInfo, error) {
	types, err := c.API.ServerType.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("error listing server types: %w", err)
	}

	var result []config.ServerTypeInfo
	for _, t := range types {
		var locations []string
		for _, loc := range t.Locations {
			if loc.Location != nil {
				locations = append(locations, loc.Location.Name)
			}
		}

		var priceMonthly float64
		if len(t.Pricings) > 0 {
			if v, err := strconv.ParseFloat(t.Pricings[0].Monthly.Gross, 64); err == nil {
				priceMonthly = v
			}
		}

		result = append(result, config.ServerTypeInfo{
			Name:         t.Name,
			Description:  t.Description,
			Cores:        t.Cores,
			Memory:       t.Memory,
			Disk:         t.Disk,
			CPUType:      string(t.CPUType),
			Architecture: string(t.Architecture),
			Locations:    locations,
			PriceMonthly: priceMonthly,
		})
	}

	return result, nil
}

func SSHConnect(ip string) (*ssh.Client, error) {
	var authMethods []ssh.AuthMethod
	var diagErrors []string

	// Try SSH agent first (handles passphrase-protected keys)
	var agentConn net.Conn
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		conn, err := net.Dial("unix", sock)
		if err != nil {
			diagErrors = append(diagErrors, fmt.Sprintf("SSH agent dial failed: %v", err))
		} else {
			agentConn = conn
			agentClient := agent.NewClient(conn)
			authMethods = append(authMethods, ssh.PublicKeysCallback(agentClient.Signers))
		}
	}

	// Fall back to raw key files for unprotected keys
	home, homeErr := os.UserHomeDir()
	if homeErr != nil {
		diagErrors = append(diagErrors, fmt.Sprintf("could not find home directory: %v", homeErr))
	} else {
		for _, name := range []string{"id_ed25519", "id_rsa"} {
			keyPath := filepath.Join(home, ".ssh", name)
			keyBytes, err := os.ReadFile(keyPath)
			if err != nil {
				continue
			}
			signer, err := ssh.ParsePrivateKey(keyBytes)
			if err != nil {
				var passErr *ssh.PassphraseMissingError
				if errors.As(err, &passErr) {
					continue // passphrase-protected, skip — agent handles these
				}
				diagErrors = append(diagErrors, fmt.Sprintf("failed to parse %s: %v", keyPath, err))
				continue
			}
			authMethods = append(authMethods, ssh.PublicKeys(signer))
		}
	}

	if len(authMethods) == 0 {
		msg := "no SSH auth available: set SSH_AUTH_SOCK or provide an unprotected key at ~/.ssh/id_ed25519 or ~/.ssh/id_rsa"
		if len(diagErrors) > 0 {
			msg += "\ndetails:\n  " + strings.Join(diagErrors, "\n  ")
		}
		return nil, fmt.Errorf("%s", msg)
	}

	config := &ssh.ClientConfig{
		User:            "root",
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	client, err := ssh.Dial("tcp", ip+":22", config)
	if err != nil {
		if agentConn != nil {
			agentConn.Close()
		}
		return nil, err
	}
	return client, nil
}

func WaitForSSH(ctx context.Context, ip string) (*ssh.Client, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		client, err := SSHConnect(ip)
		if err == nil {
			return client, nil
		}
		time.Sleep(3 * time.Second)
	}
}

func SSHRun(client *ssh.Client, cmd string) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("error creating SSH session: %w", err)
	}
	defer session.Close()

	output, err := session.CombinedOutput(cmd)
	if err != nil {
		return string(output), fmt.Errorf("command failed: %w\nOutput: %s", err, output)
	}
	return string(output), nil
}
