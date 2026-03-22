package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type ServerTypeInfo struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Cores        int      `json:"cores"`
	Memory       float32  `json:"memory"`
	Disk         int      `json:"disk"`
	CPUType      string   `json:"cpu_type"`
	Architecture string   `json:"architecture"`
	Locations    []string `json:"locations"`
	PriceMonthly float64  `json:"price_monthly"`
}

type ServerTypeCache struct {
	Types     []ServerTypeInfo `json:"types"`
	FetchedAt time.Time       `json:"fetched_at"`
}

func ServerTypeCachePath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "server_types.json"), nil
}

func LoadServerTypeCache() (*ServerTypeCache, error) {
	p, err := ServerTypeCachePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("could not read server type cache: %w", err)
	}

	var cache ServerTypeCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("invalid server type cache: %w", err)
	}

	return &cache, nil
}

func (c *ServerTypeCache) IsStale() bool {
	return time.Since(c.FetchedAt) > 7*24*time.Hour
}

func (c *ServerTypeCache) Save() error {
	dir, err := Dir()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("could not create config directory: %w", err)
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("could not marshal server type cache: %w", err)
	}

	p := filepath.Join(dir, "server_types.json")
	return os.WriteFile(p, data, 0600)
}

// ValidateServerType checks a server type name against the cache.
// Returns nil if valid, or an error with helpful suggestions.
func ValidateServerType(name, location string, cache *ServerTypeCache) error {
	if cache == nil || len(cache.Types) == 0 {
		return validateAgainstFallback(name)
	}

	for _, t := range cache.Types {
		if t.Name == name {
			if t.Architecture == "arm" {
				x86Alts := suggestX86Types(cache, location)
				return fmt.Errorf("ARM server '%s' not supported (golden snapshot is x86). Use an x86 type: %s", name, x86Alts)
			}
			if location != "" && !containsStr(t.Locations, location) {
				available := availableLocations(cache, name)
				return fmt.Errorf("server type '%s' not available in %s. Available in: %s", name, location, strings.Join(available, ", "))
			}
			return nil
		}
	}

	closest := findClosest(name, cache.Types)
	available := listAvailableTypes(cache, location)
	return fmt.Errorf("unknown server type '%s'. Did you mean '%s'?\n\nAvailable x86 types:\n%s", name, closest, available)
}

// GetMonthlyPrice returns the monthly price for a server type from the cache.
// Falls back to a rough estimate if not found.
func GetMonthlyPrice(serverType string, cache *ServerTypeCache) float64 {
	if cache != nil {
		for _, t := range cache.Types {
			if t.Name == serverType {
				return t.PriceMonthly
			}
		}
	}
	// Fallback pricing
	switch serverType {
	case "cx23":
		return 3.49
	case "cx33":
		return 5.99
	case "cx43":
		return 14.99
	case "cx53":
		return 29.99
	default:
		return 5.99
	}
}

func validateAgainstFallback(name string) error {
	for _, t := range FallbackServerTypes {
		if t.Name == name {
			if t.Architecture == "arm" {
				return fmt.Errorf("ARM server '%s' not supported (golden snapshot is x86)", name)
			}
			return nil
		}
	}
	if strings.HasPrefix(name, "cax") {
		return fmt.Errorf("ARM servers (cax*) are not supported. The golden snapshot is x86. Use cx* or ccx* server types")
	}
	return nil
}

func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

func suggestX86Types(cache *ServerTypeCache, location string) string {
	var names []string
	for _, t := range cache.Types {
		if t.Architecture != "arm" && t.CPUType == "shared" {
			if location == "" || containsStr(t.Locations, location) {
				names = append(names, t.Name)
			}
		}
	}
	if len(names) > 5 {
		names = names[:5]
	}
	return strings.Join(names, ", ")
}

func availableLocations(cache *ServerTypeCache, name string) []string {
	for _, t := range cache.Types {
		if t.Name == name {
			return t.Locations
		}
	}
	return nil
}

func listAvailableTypes(cache *ServerTypeCache, location string) string {
	var lines []string
	for _, t := range cache.Types {
		if t.Architecture == "arm" {
			continue
		}
		if location != "" && !containsStr(t.Locations, location) {
			continue
		}
		line := fmt.Sprintf("  %-8s %2d vCPU  %5.0f GB RAM  %4d GB disk  $%.2f/mo",
			t.Name, t.Cores, t.Memory, t.Disk, t.PriceMonthly)
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func findClosest(input string, types []ServerTypeInfo) string {
	best := ""
	bestDist := 999

	// Only consider x86 types for suggestions
	for _, t := range types {
		if t.Architecture == "arm" {
			continue
		}
		d := levenshtein(input, t.Name)
		if d < bestDist {
			bestDist = d
			best = t.Name
		}
	}
	if best == "" {
		return "cx33"
	}
	return best
}

func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	d := make([][]int, la+1)
	for i := range d {
		d[i] = make([]int, lb+1)
		d[i][0] = i
	}
	for j := 0; j <= lb; j++ {
		d[0][j] = j
	}
	for i := 1; i <= la; i++ {
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			d[i][j] = min(d[i-1][j]+1, min(d[i][j-1]+1, d[i-1][j-1]+cost))
		}
	}
	return d[la][lb]
}

// FallbackServerTypes is used when the API is unreachable and no cache exists.
var FallbackServerTypes = []ServerTypeInfo{
	{Name: "cx23", Cores: 2, Memory: 4, Disk: 40, CPUType: "shared", Architecture: "x86", PriceMonthly: 3.49},
	{Name: "cx33", Cores: 4, Memory: 8, Disk: 80, CPUType: "shared", Architecture: "x86", PriceMonthly: 5.99},
	{Name: "cx43", Cores: 8, Memory: 16, Disk: 160, CPUType: "shared", Architecture: "x86", PriceMonthly: 14.99},
	{Name: "cx53", Cores: 16, Memory: 32, Disk: 240, CPUType: "shared", Architecture: "x86", PriceMonthly: 29.99},
}

// SortedByPrice returns cache types sorted by monthly price.
func (c *ServerTypeCache) SortedByPrice() []ServerTypeInfo {
	sorted := make([]ServerTypeInfo, len(c.Types))
	copy(sorted, c.Types)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].PriceMonthly < sorted[j].PriceMonthly
	})
	return sorted
}

// BumpVersion increments the patch version of a semver string.
// "v0.1.0" -> "v0.2.0", "v1.2.3" -> "v1.2.4"
func BumpVersion(version string) string {
	v := strings.TrimPrefix(version, "v")
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return "v0.1.0"
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return "v0.1.0"
	}
	parts[2] = strconv.Itoa(patch + 1)
	return "v" + strings.Join(parts, ".")
}
