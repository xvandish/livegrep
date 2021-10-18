package config

import (
	"html/template"
	"time"
)

type Backend struct {
	Id   string `json:"id"`
	Addr string `json:"addr"`
}

type Honeycomb struct {
	WriteKey string `json:"write_key"`
	Dataset  string `json:"dataset"`
}

// For more options - https://pkg.go.dev/gopkg.in/alexcesaro/statsd.v2#pkg-index
type StatsD struct {
	// The location of the StatsD daemon - :8125 is the default
	Address string `json:"address"`
	// How often the Client's buffer is flushed. If 0, the buffer
	// is only flushed when it is full
	FlushPeriod time.Duration `json:"flush_period"`
	// The prefix that will be used in every bucket name
	// e.g Prefix=hello and you send 'index.bytes', 'hello.index.bytes' will be sent
	Prefix string `json:"prefix"`
	// Appends the given tags to the tags sent with every metric
	// must be set as key-value pairs. If non-even num of tags given, Tags panics
	Tags []string `json:"tags"`
	// Format of the tags. Only the strings "InfluxDB" and "Datadog" are accepted
	TagsFormat string `json:"tags_format"`
}

type Config struct {
	// Location of the directory containing templates and static
	// assets. This should point at the "web" directory of the
	// repository.
	DocRoot string `json:"docroot"`

	Feedback struct {
		// The mailto address for the "feedback" url.
		MailTo string `json:"mailto"`
	} `json:"feedback"`

	GoogleAnalyticsId string `json:"google_analytics_id"`
	// Should we respect X-Real-Ip, X-Real-Proto, and X-Forwarded-Host?
	ReverseProxy bool `json:"reverse_proxy"`

	// List of backends to connect to. Each backend must include
	// the "id" and "addr" fields.
	Backends []Backend `json:"backends"`

	// The address to listen on, as HOST:PORT.
	Listen string `json:"listen"`

	// HTML injected into layout template
	// for site-specific customizations
	HeaderHTML template.HTML `json:"header_html"`

	// HTML injected into layout template
	// just before </body> for site-specific customization
	FooterHTML template.HTML `json:"footer_html"`

	Sentry struct {
		URI string `json:"uri"`
	} `json:"sentry"`

	// Whether to re-load templates on every request
	Reload bool `json:"reload"`

	// honeycomb API write key
	Honeycomb Honeycomb `json:"honeycomb"`

	// If included, search api metrics will be sent to StatsD
	StatsD StatsD `json:"statsd"`

	DefaultMaxMatches int32 `json:"default_max_matches"`

	// Same json config structure that the backend uses when building indexes;
	// used here for repository browsing.
	IndexConfig IndexConfig `json:"index_config"`

	DefaultSearchRepos []string `json:"default_search_repos"`

	LinkConfigs []LinkConfig `json:"file_links"`
}

type IndexConfig struct {
	Name         string       `json:"name"`
	Repositories []RepoConfig `json:"repositories"`
}

type RepoConfig struct {
	Path           string            `json:"path"`
	Name           string            `json:"name"`
	Revisions      []string          `json:"revisions"`
	Metadata       map[string]string `json:"metadata"`
	WalkSubmodules bool              `json:"walk_submodules"`
}

type LinkConfig struct {
	Label            string `json:"label"`
	UrlTemplate      string `json:"url_template"`
	WhitelistPattern string `json:"whitelist_pattern"`
}
