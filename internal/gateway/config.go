package gateway

import (
	"time"

	"gopkg.in/yaml.v3"
)

// Config defines the process-level limits and the routes the gateway serves.
type Config struct {
	Listen       string `yaml:"listen"`
	Workers      int    `yaml:"workers"`
	QueueSize    int    `yaml:"queue_size"`
	MaxBodyBytes int64  `yaml:"max_body_bytes"`
	// TrustedProxies lists the CIDRs of reverse proxies in front of the gateway.
	// Only requests from them have X-Forwarded-For honored for the client IP.
	TrustedProxies []string          `yaml:"trusted_proxies"`
	Routes         []RouteConfig     `yaml:"routes"`
	RateLimits     []RateLimitConfig `yaml:"rate_limit"`
}

type RouteConfig struct {
	Name       string           `yaml:"name"`
	Pattern    string           `yaml:"pattern"` // /v1/models/{model}/predict, /v1/*, or re:^/...
	Methods    []string         `yaml:"methods"`
	Upstream   string           `yaml:"upstream"`
	Timeout    Duration         `yaml:"timeout"`
	Gatekeeper GatekeeperConfig `yaml:"gatekeeper"`
}

type GatekeeperConfig struct {
	APIKeys            []string `yaml:"api_keys"`
	Header             string   `yaml:"header"`
	MaxConcurrentCalls int      `yaml:"max_concurrent_calls"`
}

// RateLimitConfig supports token_bucket (default), fixed_window and sliding_window.
type RateLimitConfig struct {
	Route   string   `yaml:"route"`
	Pattern string   `yaml:"pattern"`
	Limit   int      `yaml:"limit"`
	Window  Duration `yaml:"window"`
	Burst   int      `yaml:"burst"`
	Key     string   `yaml:"key"` // ip, api_key, or ip_api_key (default)
}

type Duration struct{ time.Duration }

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	d.Duration = v
	return nil
}
