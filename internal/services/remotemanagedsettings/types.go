package remotemanagedsettings

import "encoding/json"

type RemoteManagedSettingsResponse struct {
	UUID     string                 `json:"uuid"`
	Checksum string                 `json:"checksum"`
	Settings map[string]interface{} `json:"settings"`
}

type RemoteManagedSettingsFetchResult struct {
	Success   bool                                  `json:"success"`
	Settings  map[string]interface{}                `json:"settings,omitempty"`
	Checksum  string                                `json:"checksum,omitempty"`
	Error     string                                `json:"error,omitempty"`
	SkipRetry bool                                  `json:"skipRetry,omitempty"`
}

type SecurityCheckResult int

const (
	SecurityCheckApproved      SecurityCheckResult = iota
	SecurityCheckRejected
	SecurityCheckNoCheckNeeded
)

var DangerousSettingKeys = map[string]bool{
	"allowedTools":         true,
	"blockedTools":         true,
	"shellCommandTimeout":  true,
	"customInstructions":   true,
	"mcpServers":           true,
	"env":                  true,
}

func HasDangerousSettings(settings map[string]interface{}) bool {
	for key := range settings {
		if DangerousSettingKeys[key] {
			return true
		}
	}
	return false
}

func HasDangerousSettingsChanged(cached, newSettings map[string]interface{}) bool {
	if cached == nil {
		return HasDangerousSettings(newSettings)
	}
	for key := range newSettings {
		if !DangerousSettingKeys[key] {
			continue
		}
		cachedVal, cachedOk := cached[key]
		newVal, newOk := newSettings[key]
		if !cachedOk || !newOk {
			return true
		}
		cachedJSON, _ := marshalJSON(cachedVal)
		newJSON, _ := marshalJSON(newVal)
		if cachedJSON != newJSON {
			return true
		}
	}
	return false
}

func CheckManagedSettingsSecurity(cachedSettings, newSettings map[string]interface{}, isInteractive bool) SecurityCheckResult {
	if newSettings == nil || !HasDangerousSettings(newSettings) {
		return SecurityCheckNoCheckNeeded
	}
	if !HasDangerousSettingsChanged(cachedSettings, newSettings) {
		return SecurityCheckNoCheckNeeded
	}
	if !isInteractive {
		return SecurityCheckNoCheckNeeded
	}
	return SecurityCheckApproved
}

func marshalJSON(v interface{}) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(data), nil
}