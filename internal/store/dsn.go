package store

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/EggyFly/Wiki-X/internal/config"
)

func primaryDSN(cfg config.DatabaseConfig) (string, error) {
	if cfg.Primary.Address != "" || cfg.Primary.Database != "" {
		return nodeDSN(cfg.Primary)
	}
	if cfg.URL != "" {
		return cfg.URL, nil
	}
	return "", errors.New("database is not configured")
}

func replicaDSN(cfg config.DatabaseConfig) (string, error) {
	if cfg.Replica.Address == "" || cfg.Replica.Database == "" {
		return "", errors.New("database replica is not configured")
	}
	return nodeDSN(cfg.Replica)
}

func nodeDSN(node config.DatabaseNode) (string, error) {
	address := strings.TrimSpace(node.Address)
	if address == "" {
		return "", errors.New("database address is empty")
	}
	if _, _, err := net.SplitHostPort(address); err != nil {
		if strings.HasPrefix(address, "[") && strings.HasSuffix(address, "]") {
			address = net.JoinHostPort(strings.TrimSuffix(strings.TrimPrefix(address, "["), "]"), "5432")
		} else if strings.Count(address, ":") > 1 {
			address = net.JoinHostPort(address, "5432")
		} else if strings.Contains(address, ":") {
			return "", fmt.Errorf("database address %q must be host:port", address)
		} else {
			address = net.JoinHostPort(address, "5432")
		}
	}
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", fmt.Errorf("database address %q must be host:port", address)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return "", fmt.Errorf("database address %q has an invalid port", address)
	}
	database := strings.TrimSpace(node.Database)
	if database == "" {
		return "", errors.New("database name is empty")
	}
	params := strings.TrimPrefix(strings.TrimSpace(node.Params), "?")
	u := &url.URL{
		Scheme:   "postgres",
		Host:     address,
		Path:     "/" + database,
		RawQuery: params,
	}
	if node.Username != "" {
		if node.Password != "" {
			u.User = url.UserPassword(node.Username, node.Password)
		} else {
			u.User = url.User(node.Username)
		}
	}
	return u.String(), nil
}
