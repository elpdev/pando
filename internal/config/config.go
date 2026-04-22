package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultRelayURL  = "ws://localhost:8080/ws"
	DefaultRelayAddr = ":8080"
	defaultRootDir   = ".pando"
)

type Client struct {
	RelayURL         string
	RelayToken       string
	RelayCAPath      string
	Mailbox          string
	RecipientMailbox string
	RootDir          string
	DataDir          string
}

type RelayProfile struct {
	Name  string `yaml:"name,omitempty"`
	URL   string `yaml:"url,omitempty"`
	Token string `yaml:"token,omitempty"`
}

type Relay struct {
	Addr               string
	RootDir            string
	StorePath          string
	QueueTTL           time.Duration
	MaxMessageBytes    int
	MaxQueuedMessages  int
	MaxQueuedBytes     int
	RateLimitPerMinute int
	AuthToken          string
	AllowedOrigins     []string
	LandingPage        bool
}

func DefaultClient() Client {
	return Client{RelayURL: DefaultRelayURL, RootDir: DefaultRootDir()}
}

func DefaultRelay() Relay {
	return Relay{
		Addr:               DefaultRelayAddr,
		RootDir:            DefaultRootDir(),
		QueueTTL:           24 * time.Hour,
		MaxMessageBytes:    64 * 1024,
		MaxQueuedMessages:  512,
		MaxQueuedBytes:     16 * 1024 * 1024,
		RateLimitPerMinute: 120,
	}
}

func (c Client) Validate() error {
	if strings.TrimSpace(c.Mailbox) == "" {
		return fmt.Errorf("mailbox is required")
	}
	if strings.TrimSpace(c.RelayURL) == "" {
		return fmt.Errorf("relay URL is required")
	}
	if strings.TrimSpace(c.DataDir) == "" {
		return fmt.Errorf("data dir is required")
	}
	return nil
}

func (r Relay) Validate() error {
	if strings.TrimSpace(r.Addr) == "" {
		return fmt.Errorf("listen address is required")
	}
	if strings.TrimSpace(r.StorePath) == "" {
		return fmt.Errorf("relay store path is required")
	}
	if r.QueueTTL <= 0 {
		return fmt.Errorf("relay queue ttl must be positive")
	}
	if r.MaxMessageBytes <= 0 {
		return fmt.Errorf("relay max message bytes must be positive")
	}
	if r.MaxQueuedMessages <= 0 {
		return fmt.Errorf("relay max queued messages must be positive")
	}
	if r.MaxQueuedBytes <= 0 {
		return fmt.Errorf("relay max queued bytes must be positive")
	}
	if r.RateLimitPerMinute <= 0 {
		return fmt.Errorf("relay rate limit per minute must be positive")
	}
	return nil
}

func DefaultRootDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", defaultRootDir)
	}
	return filepath.Join(home, defaultRootDir)
}

func ClientDataDir(rootDir, mailbox string) string {
	return filepath.Join(rootDir, "clients", mailbox)
}

func RelayStorePath(rootDir string) string {
	return filepath.Join(rootDir, "relay", "relay.db")
}

func ApplyRelayEnv(cfg *Relay) error {
	if value, ok := lookupEnvTrimmed("PANDO_RELAY_ADDR"); ok {
		cfg.Addr = value
	}
	if value, ok := lookupEnvTrimmed("PANDO_RELAY_STORE_PATH"); ok {
		cfg.StorePath = value
	}
	if value, ok := lookupEnvTrimmed("PANDO_RELAY_AUTH_TOKEN"); ok {
		cfg.AuthToken = value
	}
	if value, ok := lookupEnvTrimmed("PANDO_RELAY_QUEUE_TTL"); ok {
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return fmt.Errorf("invalid PANDO_RELAY_QUEUE_TTL %q: %w", value, err)
		}
		cfg.QueueTTL = parsed
	}
	if value, ok := lookupEnvTrimmed("PANDO_RELAY_MAX_MESSAGE_BYTES"); ok {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid PANDO_RELAY_MAX_MESSAGE_BYTES %q: %w", value, err)
		}
		cfg.MaxMessageBytes = parsed
	}
	if value, ok := lookupEnvTrimmed("PANDO_RELAY_MAX_QUEUED_MESSAGES"); ok {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid PANDO_RELAY_MAX_QUEUED_MESSAGES %q: %w", value, err)
		}
		cfg.MaxQueuedMessages = parsed
	}
	if value, ok := lookupEnvTrimmed("PANDO_RELAY_MAX_QUEUED_BYTES"); ok {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid PANDO_RELAY_MAX_QUEUED_BYTES %q: %w", value, err)
		}
		cfg.MaxQueuedBytes = parsed
	}
	if value, ok := lookupEnvTrimmed("PANDO_RELAY_RATE_LIMIT_PER_MINUTE"); ok {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid PANDO_RELAY_RATE_LIMIT_PER_MINUTE %q: %w", value, err)
		}
		cfg.RateLimitPerMinute = parsed
	}
	if value, ok := lookupEnvTrimmed("PANDO_RELAY_ALLOWED_ORIGINS"); ok {
		cfg.AllowedOrigins = splitCommaList(value)
	}
	if value, ok := lookupEnvTrimmed("PANDO_RELAY_LANDING_PAGE"); ok {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid PANDO_RELAY_LANDING_PAGE %q: %w", value, err)
		}
		cfg.LandingPage = parsed
	}
	return nil
}

func splitCommaList(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		result = append(result, trimmed)
	}
	return result
}

// DeviceConfig holds optional device-wide defaults stored in config.yml.
type DeviceConfig struct {
	RelayURL       string         `yaml:"relay_url,omitempty"`
	RelayToken     string         `yaml:"relay_token,omitempty"`
	Relays         []RelayProfile `yaml:"relays,omitempty"`
	ActiveRelay    string         `yaml:"active_relay,omitempty"`
	DefaultMailbox string         `yaml:"default_mailbox,omitempty"`
	Theme          string         `yaml:"theme,omitempty"`
	MessageTTL     time.Duration  `yaml:"message_ttl,omitempty"`
	IdleTimeout    time.Duration  `yaml:"idle_timeout,omitempty"`
}

const (
	DefaultMessageTTL = 24 * time.Hour
	MaxMessageTTL     = 24 * time.Hour
	MaxIdleTimeout    = 24 * time.Hour
)

// EffectiveMessageTTL returns the configured TTL clamped to (0, MaxMessageTTL].
// An unset or non-positive value resolves to DefaultMessageTTL; values above
// MaxMessageTTL are capped.
func (c DeviceConfig) EffectiveMessageTTL() time.Duration {
	if c.MessageTTL <= 0 {
		return DefaultMessageTTL
	}
	if c.MessageTTL > MaxMessageTTL {
		return MaxMessageTTL
	}
	return c.MessageTTL
}

// EffectiveIdleTimeout returns the configured idle disconnect timeout.
// Non-positive values disable idle disconnect; values above MaxIdleTimeout are capped.
func (c DeviceConfig) EffectiveIdleTimeout() time.Duration {
	if c.IdleTimeout <= 0 {
		return 0
	}
	if c.IdleTimeout > MaxIdleTimeout {
		return MaxIdleTimeout
	}
	return c.IdleTimeout
}

func (c DeviceConfig) RelayProfiles() []RelayProfile {
	profiles := make([]RelayProfile, 0, len(c.Relays)+1)
	seen := map[string]struct{}{}
	for _, relay := range c.Relays {
		normalized, ok := normalizeRelayProfile(relay)
		if !ok {
			continue
		}
		if _, exists := seen[normalized.Name]; exists {
			continue
		}
		seen[normalized.Name] = struct{}{}
		profiles = append(profiles, normalized)
	}
	if len(profiles) == 0 {
		if legacy, ok := normalizeRelayProfile(RelayProfile{Name: defaultRelayProfileName(c.RelayURL), URL: c.RelayURL, Token: c.RelayToken}); ok {
			profiles = append(profiles, legacy)
		}
	}
	return profiles
}

func (c DeviceConfig) ActiveRelayProfile() RelayProfile {
	profiles := c.RelayProfiles()
	if len(profiles) == 0 {
		return RelayProfile{Name: defaultRelayProfileName(DefaultRelayURL), URL: DefaultRelayURL}
	}
	active := strings.TrimSpace(c.ActiveRelay)
	if active != "" {
		for _, relay := range profiles {
			if relay.Name == active {
				return relay
			}
		}
	}
	return profiles[0]
}

func (c *DeviceConfig) SetRelayProfiles(relays []RelayProfile, active string) {
	normalized := make([]RelayProfile, 0, len(relays))
	seen := map[string]struct{}{}
	for _, relay := range relays {
		clean, ok := normalizeRelayProfile(relay)
		if !ok {
			continue
		}
		if _, exists := seen[clean.Name]; exists {
			continue
		}
		seen[clean.Name] = struct{}{}
		normalized = append(normalized, clean)
	}
	c.Relays = normalized
	if len(normalized) == 0 {
		c.ActiveRelay = ""
		c.RelayURL = ""
		c.RelayToken = ""
		return
	}
	selected := strings.TrimSpace(active)
	if selected == "" {
		selected = normalized[0].Name
	}
	for _, relay := range normalized {
		if relay.Name == selected {
			c.ActiveRelay = relay.Name
			c.RelayURL = relay.URL
			c.RelayToken = relay.Token
			return
		}
	}
	c.ActiveRelay = normalized[0].Name
	c.RelayURL = normalized[0].URL
	c.RelayToken = normalized[0].Token
}

func normalizeRelayProfile(relay RelayProfile) (RelayProfile, bool) {
	relay.Name = strings.TrimSpace(relay.Name)
	relay.URL = strings.TrimSpace(relay.URL)
	if relay.Name == "" {
		relay.Name = defaultRelayProfileName(relay.URL)
	}
	if relay.Name == "" || relay.URL == "" {
		return RelayProfile{}, false
	}
	return relay, true
}

func defaultRelayProfileName(url string) string {
	if strings.TrimSpace(url) == "" {
		return "default"
	}
	if url == DefaultRelayURL {
		return "default"
	}
	return "primary"
}

func DeviceConfigPath(rootDir string) string {
	return filepath.Join(rootDir, "config.yml")
}

// LoadDeviceConfig reads the device config file. Returns an empty config (no error) if the file doesn't exist.
func LoadDeviceConfig(rootDir string) (DeviceConfig, error) {
	data, err := os.ReadFile(DeviceConfigPath(rootDir))
	if os.IsNotExist(err) {
		return DeviceConfig{}, nil
	}
	if err != nil {
		return DeviceConfig{}, fmt.Errorf("read device config: %w", err)
	}
	var cfg DeviceConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return DeviceConfig{}, fmt.Errorf("parse device config: %w", err)
	}
	if len(cfg.Relays) > 0 {
		cfg.SetRelayProfiles(cfg.Relays, cfg.ActiveRelay)
	}
	return cfg, nil
}

// SaveDeviceConfig writes the device config file, creating rootDir if needed.
func SaveDeviceConfig(rootDir string, cfg DeviceConfig) error {
	if err := os.MkdirAll(rootDir, 0o700); err != nil {
		return fmt.Errorf("create root dir: %w", err)
	}
	cfg.SetRelayProfiles(cfg.RelayProfiles(), cfg.ActiveRelay)
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode device config: %w", err)
	}
	return os.WriteFile(DeviceConfigPath(rootDir), data, 0o600)
}

func lookupEnvTrimmed(key string) (string, bool) {
	value, ok := os.LookupEnv(key)
	if !ok {
		return "", false
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	return value, true
}
