package gateway

import (
	"encoding/json"
	"time"

	"gopkg.in/yaml.v3"
)

// Config defines the process-level limits and the routes the gateway serves.
type Config struct {
	Listen       string `json:"listen" yaml:"listen"`
	Workers      int    `json:"workers" yaml:"workers"`
	QueueSize    int    `json:"queue_size" yaml:"queue_size"`
	MaxBodyBytes int64  `json:"max_body_bytes" yaml:"max_body_bytes"`
	// TrustedProxies lists the CIDRs of reverse proxies in front of the gateway.
	// Only requests from them have X-Forwarded-For honored for the client IP.
	TrustedProxies []string          `json:"trusted_proxies" yaml:"trusted_proxies"`
	Routes         []RouteConfig     `json:"routes" yaml:"routes"`
	RateLimits     []RateLimitConfig `json:"rate_limit" yaml:"rate_limit"`
}

type RouteConfig struct {
	Name       string           `json:"name" yaml:"name"`
	Pattern    string           `json:"pattern" yaml:"pattern"` // /v1/models/{model}/predict, /v1/*, or re:^/...
	Methods    []string         `json:"methods" yaml:"methods"`
	Upstream   string           `json:"upstream" yaml:"upstream"`
	Timeout    Duration         `json:"timeout" yaml:"timeout"`
	Gatekeeper GatekeeperConfig `json:"gatekeeper" yaml:"gatekeeper"`
}

type GatekeeperConfig struct {
	APIKeys            []string `json:"api_keys" yaml:"api_keys"`
	Header             string   `json:"header" yaml:"header"`
	MaxConcurrentCalls int      `json:"max_concurrent_calls" yaml:"max_concurrent_calls"`
}

// RateLimitConfig supports token_bucket (default), fixed_window and sliding_window.
type RateLimitConfig struct {
	Route   string   `json:"route" yaml:"route"`
	Pattern string   `json:"pattern" yaml:"pattern"`
	Limit   int      `json:"limit" yaml:"limit"`
	Window  Duration `json:"window" yaml:"window"`
	Burst   int      `json:"burst" yaml:"burst"`
	Key     string   `json:"key" yaml:"key"` // ip, api_key, or ip_api_key (default)
}

type Duration struct{ time.Duration }

func (d *Duration) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	d.Duration = v
	return nil
}

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
