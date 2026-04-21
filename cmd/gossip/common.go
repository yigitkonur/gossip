package main

import (
	"encoding/json"
	"os"
	"strconv"

	"github.com/yigitkonur/gossip/internal/statedir"
)

func controlPort() int {
	if port, ok := firstPositiveIntEnv("GOSSIP_CONTROL_PORT"); ok {
		return port
	}
	return 4502
}

func firstPositiveIntEnv(keys ...string) (int, bool) {
	for _, key := range keys {
		raw := os.Getenv(key)
		if raw == "" {
			continue
		}
		n, err := strconv.Atoi(raw)
		if err == nil && n > 0 {
			return n, true
		}
	}
	return 0, false
}

type daemonStatusFile struct {
	ProxyURL     string `json:"proxyUrl"`
	AppServerURL string `json:"appServerUrl"`
	ControlPort  int    `json:"controlPort"`
	Pid          int    `json:"pid"`
}

type daemonPortsFile struct {
	ControlPort int `json:"controlPort"`
	AppPort     int `json:"appPort"`
	ProxyPort   int `json:"proxyPort"`
}

func readDaemonStatus(path string) (daemonStatusFile, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return daemonStatusFile{}, false
	}
	var status daemonStatusFile
	if err := json.Unmarshal(b, &status); err != nil {
		return daemonStatusFile{}, false
	}
	return status, status.ProxyURL != ""
}

func readDaemonPorts(path string) (daemonPortsFile, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return daemonPortsFile{}, false
	}
	var ports daemonPortsFile
	if err := json.Unmarshal(b, &ports); err != nil {
		return daemonPortsFile{}, false
	}
	return ports, ports.ControlPort > 0
}

func resolvedControlPort(sd *statedir.StateDir) int {
	if ports, ok := readDaemonPorts(sd.PortsFile()); ok {
		return ports.ControlPort
	}
	return controlPort()
}
