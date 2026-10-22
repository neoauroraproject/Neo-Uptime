package store

// Heartbeat status values. They match Uptime Kuma's so push scripts and
// muscle memory carry over.
const (
	StatusDown        = 0
	StatusUp          = 1
	StatusPending     = 2
	StatusMaintenance = 3
)

// User is an account that can sign in to the admin panel.
type User struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"`
	TOTPSecret   string `json:"-"`
	TOTPEnabled  bool   `json:"totpEnabled"`
	IsOwner      bool   `json:"isOwner"`
	CreatedAt    int64  `json:"createdAt"`
	LastLogin    int64  `json:"lastLogin"`
}

// Session is a signed-in browser.
type Session struct {
	TokenHash string `json:"-"`
	ID        string `json:"id"` // short public id derived from the hash
	UserID    int64  `json:"userId"`
	CreatedAt int64  `json:"createdAt"`
	ExpiresAt int64  `json:"expiresAt"`
	LastSeen  int64  `json:"lastSeen"`
	IP        string `json:"ip"`
	UserAgent string `json:"userAgent"`
}

// APIKey is a bearer credential for scripts, scrapers and automation.
type APIKey struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Prefix    string `json:"prefix"`
	KeyHash   string `json:"-"`
	Scope     string `json:"scope"`
	UserID    int64  `json:"userId"`
	CreatedAt int64  `json:"createdAt"`
	ExpiresAt int64  `json:"expiresAt"`
	LastUsed  int64  `json:"lastUsed"`
	Active    bool   `json:"active"`
}

// MonitorConfig holds the type-specific settings of a monitor. One struct for
// every type keeps the storage format flat and the API predictable; each
// checker reads only the fields it understands.
type MonitorConfig struct {
	// HTTP(s), keyword and JSON query.
	URL                 string   `json:"url,omitempty"`
	Method              string   `json:"method,omitempty"`
	Headers             string   `json:"headers,omitempty"`
	Body                string   `json:"body,omitempty"`
	BodyEncoding        string   `json:"bodyEncoding,omitempty"`
	AcceptedStatusCodes []string `json:"acceptedStatusCodes,omitempty"`
	MaxRedirects        *int     `json:"maxRedirects,omitempty"`
	IgnoreTLS           bool     `json:"ignoreTls,omitempty"`
	BasicAuthUser       string   `json:"basicAuthUser,omitempty"`
	BasicAuthPass       string   `json:"basicAuthPass,omitempty"`
	Keyword             string   `json:"keyword,omitempty"`
	InvertKeyword       bool     `json:"invertKeyword,omitempty"`
	JSONPath            string   `json:"jsonPath,omitempty"`
	JSONOperator        string   `json:"jsonOperator,omitempty"`
	ExpectedValue       string   `json:"expectedValue,omitempty"`
	CertExpiryNotify    bool     `json:"certExpiryNotify,omitempty"`

	// TCP port, ping and DNS.
	Hostname      string `json:"hostname,omitempty"`
	Port          int    `json:"port,omitempty"`
	PacketCount   int    `json:"packetCount,omitempty"`
	DNSResolver   string `json:"dnsResolver,omitempty"`
	DNSRecordType string `json:"dnsRecordType,omitempty"`

	// Docker container.
	ContainerName string `json:"containerName,omitempty"`
	DockerHost    string `json:"dockerHost,omitempty"`

	// Databases.
	ConnectionString string `json:"connectionString,omitempty"`
	Query            string `json:"query,omitempty"`

	// BackPack tunnel / panel.
	PanelID           int64  `json:"panelId,omitempty"`
	TunnelName        string `json:"tunnelName,omitempty"`
	ServiceDownIsDown bool   `json:"serviceDownIsDown,omitempty"`

	// Agent server / agent tunnel (BackPack, Kariz or NeoTunnel through neo-uptime-agent).
	AgentID int64  `json:"agentId,omitempty"`
	Source  string `json:"source,omitempty"` // backpack | kariz | neotunnel
	// TunnelLabel is the tunnel's display name in its program when the
	// monitor was last named after it. While the monitor still carries that
	// name, a new display name renames the monitor too; once someone names
	// it themselves, it stays.
	TunnelLabel string `json:"tunnelLabel,omitempty"`

	// Other protocols.
	Username    string `json:"username,omitempty"`
	Password    string `json:"password,omitempty"`
	Topic       string `json:"topic,omitempty"`       // MQTT
	Secret      string `json:"secret,omitempty"`      // RADIUS
	Community   string `json:"community,omitempty"`   // SNMP
	OID         string `json:"oid,omitempty"`         // SNMP
	SNMPVersion string `json:"snmpVersion,omitempty"` // 1 | 2c
	Service     string `json:"service,omitempty"`     // gRPC health service name
	Security    string `json:"security,omitempty"`    // SMTP: none | starttls | tls ; gRPC: tls
	Payload     string `json:"payload,omitempty"`     // UDP
	WarnDays    int    `json:"warnDays,omitempty"`    // TLS certificate monitor
	// V2Ray / Xray config, tested through a real Xray core. AgentID, when
	// set, is the server that runs the test (0 = this panel).
	Link           string `json:"link,omitempty"`
	TestURL        string `json:"testUrl,omitempty"`
	SubscriptionID int64  `json:"subscriptionId,omitempty"`
	ConfigName     string `json:"configName,omitempty"` // its name in the subscription
	Gone           bool   `json:"gone,omitempty"`       // no longer in the subscription

	// Latency alert: notify when the response time stays above LatencyAlertMs
	// for LatencyAlertFor seconds (and again when it recovers). 0 turns it off.
	LatencyAlertMs  int `json:"latencyAlertMs,omitempty"`
	LatencyAlertFor int `json:"latencyAlertFor,omitempty"`
}

// Monitor is one thing being watched.
type Monitor struct {
	ID              int64         `json:"id"`
	Name            string        `json:"name"`
	Type            string        `json:"type"`
	ParentID        *int64        `json:"parentId"`
	Active          bool          `json:"active"`
	Interval        int           `json:"interval"`
	RetryInterval   int           `json:"retryInterval"`
	MaxRetries      int           `json:"maxRetries"`
	Timeout         int           `json:"timeout"`
	ResendInterval  int           `json:"resendInterval"`
	UpsideDown      bool          `json:"upsideDown"`
	Description     string        `json:"description"`
	Config          MonitorConfig `json:"config"`
	Country         string        `json:"country"`
	PushToken       string        `json:"pushToken,omitempty"`
	SortOrder       int           `json:"sortOrder"`
	CreatedAt       int64         `json:"createdAt"`
	UpdatedAt       int64         `json:"updatedAt"`
	Tags            []MonitorTag  `json:"tags"`
	NotificationIDs []int64       `json:"notificationIds"`
}

// Heartbeat is the result of one check.
type Heartbeat struct {
	ID        int64    `json:"id"`
	MonitorID int64    `json:"monitorId"`
	Status    int      `json:"status"`
	Time      int64    `json:"time"` // unix milliseconds
	Ping      *float64 `json:"ping"` // milliseconds
	Msg       string   `json:"msg"`
	Important bool     `json:"important"`
	Duration  int64    `json:"duration"` // seconds since the previous beat
	BytesIn   *int64   `json:"bytesIn,omitempty"`
	BytesOut  *int64   `json:"bytesOut,omitempty"`
}

// Tag is a coloured label.
type Tag struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	CreatedAt int64  `json:"createdAt"`
}

// MonitorTag is a tag attached to a monitor, optionally with a value.
type MonitorTag struct {
	TagID int64  `json:"tagId"`
	Name  string `json:"name"`
	Color string `json:"color"`
	Value string `json:"value"`
}

// Notification is a configured alert channel.
type Notification struct {
	ID        int64             `json:"id"`
	Name      string            `json:"name"`
	Type      string            `json:"type"`
	Config    map[string]string `json:"config"`
	IsDefault bool              `json:"isDefault"`
	Active    bool              `json:"active"`
	CreatedAt int64             `json:"createdAt"`
}

// StatusPage is a public page showing a selection of monitors.
type StatusPage struct {
	ID              int64             `json:"id"`
	Slug            string            `json:"slug"`
	Title           string            `json:"title"`
	Description     string            `json:"description"`
	Theme           string            `json:"theme"`
	Layout          string            `json:"layout"`
	BarMode         string            `json:"barMode"`
	AccentColor     string            `json:"accentColor"`
	Logo            string            `json:"logo"`
	Footer          string            `json:"footer"`
	CustomCSS       string            `json:"customCss"`
	ShowTags        bool              `json:"showTags"`
	ShowCertExpiry  bool              `json:"showCertExpiry"`
	ShowPoweredBy   bool              `json:"showPoweredBy"`
	ShowCharts      bool              `json:"showCharts"`
	Published       bool              `json:"published"`
	RefreshInterval int               `json:"refreshInterval"`
	Language        string            `json:"language"` // auto | en | fa
	ShowUptime      bool              `json:"showUptime"`
	ShowLatency     bool              `json:"showLatency"`
	ShowBanner      bool              `json:"showBanner"`
	Domain          string            `json:"domain"`  // own host name, e.g. status.example.com
	BarDays         int               `json:"barDays"` // days shown when BarMode is "days"
	Hide            []string          `json:"hide"`    // details visitors do not see, e.g. "port"
	Radar           StatusRadar       `json:"radar"`   // the internet radar strip
	CreatedAt       int64             `json:"createdAt"`
	UpdatedAt       int64             `json:"updatedAt"`
	Groups          []StatusPageGroup `json:"groups"`
}

// StatusRadar is a status page's internet radar strip (Chabokan's radar,
// read by the panel): whether it shows, and which datacenters (by slug;
// none means every one).
type StatusRadar struct {
	Show        bool     `json:"show"`
	Datacenters []string `json:"datacenters"`
}

// Hidden reports whether the page leaves out a detail, such as "port".
func (p *StatusPage) Hidden(key string) bool {
	for _, k := range p.Hide {
		if k == key {
			return true
		}
	}
	return false
}

// StatusPageGroup is a titled section of a status page.
type StatusPageGroup struct {
	ID       int64               `json:"id"`
	Name     string              `json:"name"`
	Monitors []StatusPageMonitor `json:"monitors"`
}

// StatusPageMonitor places a monitor in a group.
type StatusPageMonitor struct {
	MonitorID int64 `json:"monitorId"`
	ShowURL   bool  `json:"showUrl"`
}

// Incident is a message pinned to a status page.
type Incident struct {
	ID           int64  `json:"id"`
	StatusPageID int64  `json:"statusPageId"`
	Title        string `json:"title"`
	Content      string `json:"content"`
	Style        string `json:"style"`
	Pinned       bool   `json:"pinned"`
	CreatedAt    int64  `json:"createdAt"`
	UpdatedAt    int64  `json:"updatedAt"`
}

// Maintenance is a planned window during which monitors report maintenance
// instead of down and stay quiet.
type Maintenance struct {
	ID            int64   `json:"id"`
	Title         string  `json:"title"`
	Description   string  `json:"description"`
	Strategy      string  `json:"strategy"` // manual | single | weekly | monthly
	Active        bool    `json:"active"`
	StartAt       int64   `json:"startAt"` // unix seconds
	EndAt         int64   `json:"endAt"`
	Weekdays      []int   `json:"weekdays"`    // 0 = Sunday
	DaysOfMonth   []int   `json:"daysOfMonth"` // 1..31
	TimeFrom      string  `json:"timeFrom"`    // HH:MM
	TimeTo        string  `json:"timeTo"`
	Timezone      string  `json:"timezone"`
	CreatedAt     int64   `json:"createdAt"`
	UpdatedAt     int64   `json:"updatedAt"`
	MonitorIDs    []int64 `json:"monitorIds"`
	StatusPageIDs []int64 `json:"statusPageIds"`
}

// BackpackPanel is a BackPack web panel this instance reads tunnels from.
type BackpackPanel struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	URL            string `json:"url"`
	Token          string `json:"token,omitempty"`
	IgnoreTLS      bool   `json:"ignoreTls"`
	Interval       int    `json:"interval"`
	AutoImport     bool   `json:"autoImport"`
	GroupMonitorID *int64 `json:"groupMonitorId"`
	CreatedAt      int64  `json:"createdAt"`
	UpdatedAt      int64  `json:"updatedAt"`
}

// Agent is a server running neo-uptime-agent.
type Agent struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	TokenHash      string `json:"-"`
	TokenPrefix    string `json:"tokenPrefix"`
	Country        string `json:"country"`
	Interval       int    `json:"interval"`
	AutoImport     bool   `json:"autoImport"`
	GroupMonitorID *int64 `json:"groupMonitorId"`
	CreatedAt      int64  `json:"createdAt"`
	LastSeen       int64  `json:"lastSeen"`
	LastIP         string `json:"lastIp"`
	Version        string `json:"version"`
	Hostname       string `json:"hostname"`
}

// Subscription is a V2Ray subscription link: its configs are monitors of
// type "xray", kept in step with the link.
type Subscription struct {
	ID             int64            `json:"id"`
	Name           string           `json:"name"`
	URL            string           `json:"url"`
	AgentID        int64            `json:"agentId"` // where configs are tested; 0 = this panel
	Interval       int              `json:"interval"`
	Timeout        int              `json:"timeout"`
	TestURL        string           `json:"testUrl"`
	AutoImport     bool             `json:"autoImport"`
	RefreshMinutes int              `json:"refreshMinutes"`
	GroupMonitorID *int64           `json:"groupMonitorId"`
	Skip           []string         `json:"skip"` // config names not to watch
	Info           SubscriptionInfo `json:"info"`
	LastFetch      int64            `json:"lastFetch"`
	LastError      string           `json:"lastError"`
	CreatedAt      int64            `json:"createdAt"`
	UpdatedAt      int64            `json:"updatedAt"`
}

// SubscriptionInfo is what the last fetch found.
type SubscriptionInfo struct {
	Title    string `json:"title,omitempty"`
	Upload   uint64 `json:"upload,omitempty"`
	Download uint64 `json:"download,omitempty"`
	Total    uint64 `json:"total,omitempty"`
	Expire   int64  `json:"expire,omitempty"`
	HasUsage bool   `json:"hasUsage,omitempty"`
	Configs  int    `json:"configs"`
	Unusable int    `json:"unusable,omitempty"`
	Via      string `json:"via,omitempty"` // "panel" or the server that fetched it
}

// StatusAPIKey grants read access to one status page's data over the private
// status API, for showing it on another site.
type StatusAPIKey struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	Prefix         string `json:"prefix"`
	KeyHash        string `json:"-"`
	StatusPageID   int64  `json:"statusPageId"`
	AllowedOrigins string `json:"allowedOrigins"`
	CreatedAt      int64  `json:"createdAt"`
	LastUsed       int64  `json:"lastUsed"`
	Active         bool   `json:"active"`
}
