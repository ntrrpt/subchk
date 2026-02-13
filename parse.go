package main

import (
	"fmt"
	"pira/x2j/models"
	"pira/x2j/url"
	"strings"
)

// stolen from pira/x2j/main.go
func parseUrl(v2rayURL string, port int, dns string, remarks string) (*models.V2RayConfig, error) {

	// Handle URL to JSON conversion (existing functionality)
	// Parse the V2Ray URL and convert to Xray JSON
	config, err := url.ParseV2RayURL(v2rayURL)
	if err != nil {
		return nil, fmt.Errorf("error parsing URL: %v", err)
	}

	// Update the inbound port if a custom port is specified (default: 1080)
	if port != 0 {
		for i := range config.Inbounds {
			if config.Inbounds[i].Tag == "in_proxy" {
				config.Inbounds[i].Port = port
				break
			}
		}
	}

	// Update DNS servers if custom DNS is specified
	if dns != "" {
		if dns == "\"\"" || dns == "''" {
			// Empty DNS - clear the servers list
			config.DNS.Servers = []string{}
		} else {
			// Parse comma-separated DNS servers
			dnsServers := strings.Split(dns, ",")
			// Trim whitespace from each server
			for i, server := range dnsServers {
				dnsServers[i] = strings.TrimSpace(server)
			}
			config.DNS.Servers = dnsServers
		}
	}

	// Set remarks if provided
	if remarks != "" {
		config.Remarks = remarks
	}

	return config, nil
}
