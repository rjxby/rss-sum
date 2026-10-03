package config

import "fmt"

const defaultHTTPAddr = ":8080"

type RuntimeSettings struct {
	RunMigration      bool
	HTTPServerEnabled bool
	RSSWorkerEnabled  bool
	HTTPAddr          string
}

func ParseRuntimeSettings() (*RuntimeSettings, error) {
	runMigration, err := Bool(EnvRunMigration, false)
	if err != nil {
		return nil, err
	}

	httpServerEnabled, err := Bool(EnvHTTPServerEnabled, true)
	if err != nil {
		return nil, err
	}

	rssWorkerEnabled, err := Bool(EnvRSSWorkerEnabled, true)
	if err != nil {
		return nil, err
	}

	if !httpServerEnabled && !rssWorkerEnabled {
		return nil, fmt.Errorf("%s and %s cannot both be false", EnvHTTPServerEnabled, EnvRSSWorkerEnabled)
	}

	return &RuntimeSettings{
		RunMigration:      runMigration,
		HTTPServerEnabled: httpServerEnabled,
		RSSWorkerEnabled:  rssWorkerEnabled,
		HTTPAddr:          OptionalString(EnvHTTPAddr, defaultHTTPAddr),
	}, nil
}
