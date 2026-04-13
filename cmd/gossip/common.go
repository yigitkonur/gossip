package main

import (
	"encoding/json"
	"os"
	"strconv"
)

func controlPort() int {
	if raw := os.Getenv("GOSSIP_CONTROL_PORT"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	return 4502
}

type daemonStatusFile struct {
	ProxyURL     string `json:"proxyUrl"`
	AppServerURL string `json:"appServerUrl"`
	ControlPort  int    `json:"controlPort"`
	Pid          int    `json:"pid"`
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
