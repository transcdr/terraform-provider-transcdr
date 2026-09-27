package client

import "encoding/json"

// ConnectionConfig holds the non-secret settings of a connection. Which fields apply depends on the
// kind; the API ignores the rest. Storage configs also echo `kind`, which is ignored here.
type ConnectionConfig struct {
	Endpoint           *string `json:"endpoint,omitempty"`
	Bucket             *string `json:"bucket,omitempty"`
	Region             *string `json:"region,omitempty"`
	PathStyle          *bool   `json:"path_style,omitempty"`
	Account            *string `json:"account,omitempty"`
	Host               *string `json:"host,omitempty"`
	Port               *int64  `json:"port,omitempty"`
	Username           *string `json:"username,omitempty"`
	Root               *string `json:"root,omitempty"`
	Passive            *bool   `json:"passive,omitempty"`
	HostKeyFingerprint *string `json:"host_key_fingerprint,omitempty"`
	QueueURL           *string `json:"queue_url,omitempty"`
	TopicARN           *string `json:"topic_arn,omitempty"`
	URL                *string `json:"url,omitempty"`
	MessageGroupID     *string `json:"message_group_id,omitempty"`
}

// ConnectionSecrets are write-only credentials. The API never returns them.
type ConnectionSecrets struct {
	AccessKeyID          *string `json:"access_key_id,omitempty"`
	SecretAccessKey      *string `json:"secret_access_key,omitempty"`
	SessionToken         *string `json:"session_token,omitempty"`
	Password             *string `json:"password,omitempty"`
	PrivateKey           *string `json:"private_key,omitempty"`
	PrivateKeyPassphrase *string `json:"private_key_passphrase,omitempty"`
	ServiceAccountJSON   *string `json:"service_account_json,omitempty"`
	AccountKey           *string `json:"account_key,omitempty"`
	SASToken             *string `json:"sas_token,omitempty"`
	BearerToken          *string `json:"bearer_token,omitempty"`
}

// ConnectionCapabilities says what a connection can be used for.
type ConnectionCapabilities struct {
	Source      bool `json:"source"`
	Destination bool `json:"destination"`
	Watch       bool `json:"watch"`
	Trigger     bool `json:"trigger"`
	Events      bool `json:"events"`
}

// Connection is a storage or messaging connection.
type Connection struct {
	ID             string                 `json:"id"`
	Name           string                 `json:"name"`
	Kind           string                 `json:"kind"`
	Config         ConnectionConfig       `json:"config"`
	SecretsSet     []string               `json:"secrets_set"`
	Capabilities   ConnectionCapabilities `json:"capabilities"`
	Status         string                 `json:"status"`
	Class          string                 `json:"class"`
	Enabled        bool                   `json:"enabled"`
	FailureCount   int64                  `json:"failure_count"`
	DisabledReason *string                `json:"disabled_reason"`
	DisabledAt     *string                `json:"disabled_at"`
	LastError      *string                `json:"last_error"`
	LastCheckedAt  *string                `json:"last_checked_at"`
	CreatedAt      string                 `json:"created_at"`
	UpdatedAt      string                 `json:"updated_at"`
}

// AutomationSource is where an automation's files are.
type AutomationSource struct {
	ConnectionID string `json:"connection_id"`
	Prefix       string `json:"prefix"`
	Pattern      string `json:"pattern"`
}

// Destination is where outputs are delivered.
type Destination struct {
	ConnectionID string `json:"connection_id"`
	Prefix       string `json:"prefix"`
}

// Automation turns files that land in a connection into jobs.
type Automation struct {
	ID                  string            `json:"id"`
	Name                string            `json:"name"`
	Enabled             bool              `json:"enabled"`
	Trigger             string            `json:"trigger"`
	TriggerConnectionID *string           `json:"trigger_connection_id"`
	Source              AutomationSource  `json:"source"`
	PollIntervalSeconds int64             `json:"poll_interval_seconds"`
	SettleSeconds       int64             `json:"settle_seconds"`
	Preset              *string           `json:"preset"`
	Output              json.RawMessage   `json:"output"`
	Destination         *Destination      `json:"destination"`
	AfterSuccess        string            `json:"after_success"`
	Priority            string            `json:"priority"`
	Metadata            map[string]string `json:"metadata"`
	WebhookURL          *string           `json:"webhook_url"`
	HookURL             *string           `json:"hook_url"`
	JobsCreated         int64             `json:"jobs_created"`
	LastPolledAt        *string           `json:"last_polled_at"`
	LastTriggeredAt     *string           `json:"last_triggered_at"`
	LastError           *string           `json:"last_error"`
	CreatedAt           string            `json:"created_at"`
	UpdatedAt           string            `json:"updated_at"`
}

// AutomationItem is one processed source object.
type AutomationItem struct {
	Path      string  `json:"path"`
	SizeBytes *int64  `json:"size_bytes"`
	Status    string  `json:"status"`
	JobID     *string `json:"job_id"`
	Error     *string `json:"error"`
	CreatedAt string  `json:"created_at"`
}

// AutomationRun is the result of `POST /v1/automations/{id}/run`.
type AutomationRun struct {
	JobsCreated      int64    `json:"jobs_created"`
	JobIDs           []string `json:"job_ids"`
	MessagesReceived *int64   `json:"messages_received"`
	MessagesDeleted  *int64   `json:"messages_deleted"`
}

// WebhookAWS is the AWS side of an sns or sqs event destination, as returned.
type WebhookAWS struct {
	Region             string  `json:"region"`
	AccessKeyID        string  `json:"access_key_id"`
	Endpoint           *string `json:"endpoint"`
	MessageGroupID     *string `json:"message_group_id"`
	SecretAccessKeySet bool    `json:"secret_access_key_set"`
}

// WebhookEndpoint is an event destination (`/v1/webhooks`).
type WebhookEndpoint struct {
	ID             string      `json:"id"`
	Type           string      `json:"type"`
	URL            string      `json:"url"`
	TopicARN       *string     `json:"topic_arn"`
	QueueURL       *string     `json:"queue_url"`
	AWS            *WebhookAWS `json:"aws"`
	Description    string      `json:"description"`
	Events         []string    `json:"events"`
	Enabled        bool        `json:"enabled"`
	Secret         *string     `json:"secret"`
	FailureCount   int64       `json:"failure_count"`
	LastDeliveryAt *string     `json:"last_delivery_at"`
	ConnectionID   *string     `json:"connection_id"`
	CreatedAt      string      `json:"created_at"`
	UpdatedAt      *string     `json:"updated_at"`
}

// WebhookAWSInput is the AWS side of an sns or sqs event destination, as sent.
type WebhookAWSInput struct {
	AccessKeyID     *string `json:"access_key_id,omitempty"`
	SecretAccessKey *string `json:"secret_access_key,omitempty"`
	Region          *string `json:"region,omitempty"`
	Endpoint        *string `json:"endpoint,omitempty"`
	MessageGroupID  *string `json:"message_group_id,omitempty"`
}

// WebhookInput creates or updates an event destination.
type WebhookInput struct {
	Type         *string          `json:"type,omitempty"`
	URL          *string          `json:"url,omitempty"`
	TopicARN     *string          `json:"topic_arn,omitempty"`
	QueueURL     *string          `json:"queue_url,omitempty"`
	AWS          *WebhookAWSInput `json:"aws,omitempty"`
	ConnectionID *string          `json:"connection_id,omitempty"`
	Events       []string         `json:"events,omitempty"`
	Description  *string          `json:"description,omitempty"`
	Enabled      *bool            `json:"enabled,omitempty"`
}

// Preset is a named output specification: a system one (its id is its slug) or the organization's.
type Preset struct {
	ID          string            `json:"id"`
	Slug        string            `json:"slug"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	System      bool              `json:"system"`
	Output      json.RawMessage   `json:"output"`
	Metadata    map[string]string `json:"metadata"`
	CreatedAt   *string           `json:"created_at"`
	UpdatedAt   *string           `json:"updated_at"`
}

// APIKey is a secret API key. Secret is present only in the create response.
type APIKey struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Prefix     string   `json:"prefix"`
	Scopes     []string `json:"scopes"`
	Mode       string   `json:"mode"`
	LastUsedAt *string  `json:"last_used_at"`
	ExpiresAt  *string  `json:"expires_at"`
	CreatedAt  string   `json:"created_at"`
	Secret     *string  `json:"secret"`
}

// Plan is the public part of a plan.
type Plan struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	MaxConcurrentJobs int64    `json:"max_concurrent_jobs"`
	MaxResolution     int64    `json:"max_resolution"`
	Priority          bool     `json:"priority"`
	RetentionDays     int64    `json:"retention_days"`
	Features          []string `json:"features"`
}

// Organization is the organization the key belongs to.
type Organization struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Slug         string  `json:"slug"`
	Plan         string  `json:"plan"`
	BillingEmail *string `json:"billing_email"`
	Suspended    *bool   `json:"suspended"`
	PlanDetails  *Plan   `json:"plan_details"`
	CreatedAt    string  `json:"created_at"`
}
