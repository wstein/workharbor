// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"github.com/wstein/workharbor-nxt/apps/api_client/wirejson"
	"strings"
)

func stringField(m map[string]any, key string) (string, bool) { v, ok := m[key].(string); return v, ok }
func objectField(m map[string]any, key string) (map[string]any, bool) {
	v, ok := m[key].(map[string]any)
	return v, ok
}
func nativeContainers(raw []byte) ([]Container, error) {
	value, e := wirejson.Parse(raw)
	if e != nil {
		return nil, fail("runtime_unknown")
	}
	values, ok := value.([]any)
	if !ok {
		return nil, fail("runtime_unknown")
	}
	all := make([]Container, 0, len(values))
	for _, value := range values {
		m, ok := value.(map[string]any)
		if !ok {
			return nil, fail("runtime_unknown")
		}
		var c Container
		var id, name, image, user, userns, running, mountsOK bool
		c.ID, id = stringField(m, "Id")
		c.Name, name = stringField(m, "Name")
		c.Image, image = stringField(m, "Image")
		cfg, cfgOK := objectField(m, "Config")
		host, hostOK := objectField(m, "HostConfig")
		state, stateOK := objectField(m, "State")
		c.User, user = stringField(cfg, "User")
		c.UsernsMode, userns = stringField(host, "UsernsMode")
		c.Running, running = state["Running"].(bool)
		mounts, mountsOK := m["Mounts"].([]any)
		if !id || !name || !image || !cfgOK || !hostOK || !stateOK || !user || !userns || !running || !mountsOK || !hexID.MatchString(c.ID) || !strings.HasPrefix(c.Image, "sha256:") || !hexID.MatchString(strings.TrimPrefix(c.Image, "sha256:")) {
			return nil, fail("runtime_unknown")
		}
		c.Labels = map[string]string{}
		if value, exists := cfg["Labels"]; exists && value != nil {
			labels, ok := value.(map[string]any)
			if !ok {
				return nil, fail("runtime_unknown")
			}
			for key, value := range labels {
				label, ok := value.(string)
				if !ok {
					return nil, fail("runtime_unknown")
				}
				c.Labels[key] = label
			}
		}
		c.Mounts = make([]Mount, 0, len(mounts))
		for _, value := range mounts {
			m, ok := value.(map[string]any)
			if !ok {
				return nil, fail("runtime_unknown")
			}
			var mount Mount
			var typ, source, destination, rw bool
			mount.Type, typ = stringField(m, "Type")
			mount.Source, source = stringField(m, "Source")
			mount.Destination, destination = stringField(m, "Destination")
			mount.RW, rw = m["RW"].(bool)
			if !typ || !source || !destination || !rw {
				return nil, fail("runtime_unknown")
			}
			c.Mounts = append(c.Mounts, mount)
		}
		all = append(all, c)
	}
	return all, nil
}
func nativeInformation(raw []byte) (EngineInfo, error) {
	value, e := wirejson.Parse(raw)
	if e != nil {
		return EngineInfo{}, fail("provider_unavailable")
	}
	m, ok := value.(map[string]any)
	if !ok {
		return EngineInfo{}, fail("provider_unavailable")
	}
	var info EngineInfo
	var os, arch, version bool
	info.OSType, os = stringField(m, "OSType")
	info.Architecture, arch = stringField(m, "Architecture")
	info.ServerVersion, version = stringField(m, "ServerVersion")
	options, ok := m["SecurityOptions"].([]any)
	if !os || !arch || !version || !ok {
		return EngineInfo{}, fail("provider_unavailable")
	}
	info.SecurityOptions = make([]string, 0, len(options))
	for _, value := range options {
		s, ok := value.(string)
		if !ok {
			return EngineInfo{}, fail("provider_unavailable")
		}
		info.SecurityOptions = append(info.SecurityOptions, s)
	}
	return info, nil
}
