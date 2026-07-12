package capture

import "strings"

// CapabilityStatus summarizes what capture support is available.
type CapabilityStatus string

const (
	CapabilityUnknown     CapabilityStatus = "unknown"
	CapabilityUnavailable CapabilityStatus = "unavailable"
	CapabilityReady       CapabilityStatus = "ready"
	CapabilityDegraded    CapabilityStatus = "degraded"
)

func (s CapabilityStatus) Valid() bool {
	switch s {
	case CapabilityUnknown, CapabilityUnavailable, CapabilityReady, CapabilityDegraded:
		return true
	}
	return false
}

// CapabilityIssue describes one reason capture support is limited. Messages
// are fixed safe phrases, never raw driver error dumps.
type CapabilityIssue struct {
	Code    string
	Message string
}

// Capability reports discovered capture support. Results are value types; use
// Clone before sharing a stored Capability so callers cannot mutate internal
// state. No implementation may claim hardware exists unless it detected it.
type Capability struct {
	Status          CapabilityStatus
	AudioAvailable  bool
	VideoAvailable  bool
	PauseSupported  bool
	ResumeSupported bool
	Devices         []Device
	DefaultDeviceID string
	Issues          []CapabilityIssue
}

// Clone returns a deep copy so shared capability values stay immutable.
func (c Capability) Clone() Capability {
	result := c
	result.Devices = append([]Device(nil), c.Devices...)
	result.Issues = append([]CapabilityIssue(nil), c.Issues...)
	return result
}

// Validate enforces the capability contract:
//   - unknown/unavailable capabilities claim no hardware at all
//   - ready requires at least one available device and no issues
//   - degraded requires at least one issue explaining the limitation
//   - audio/video availability flags must agree with the device list
//   - resume support requires pause support
//   - the default device ID must reference a listed default device
//   - devices and issues follow the stable-ordering contract
func (c Capability) Validate() error {
	invalid := func(message string) error {
		return NewError(ErrorInvalidRequest, OpCapabilities, false, OutcomeNotStarted, message, nil)
	}
	if !c.Status.Valid() {
		return invalid("unknown capability status")
	}
	if err := ValidateDevices(c.Devices); err != nil {
		return err
	}
	if err := validateIssues(c.Issues); err != nil {
		return err
	}
	if c.Status == CapabilityUnknown || c.Status == CapabilityUnavailable {
		if c.AudioAvailable || c.VideoAvailable || len(c.Devices) != 0 || c.DefaultDeviceID != "" {
			return invalid("capability without support cannot claim devices")
		}
		if c.PauseSupported || c.ResumeSupported {
			return invalid("capability without support cannot claim pause or resume")
		}
		return nil
	}
	if c.Status == CapabilityReady && len(c.Issues) != 0 {
		return invalid("ready capability cannot carry issues")
	}
	if c.Status == CapabilityDegraded && len(c.Issues) == 0 {
		return invalid("degraded capability requires at least one issue")
	}
	if c.ResumeSupported && !c.PauseSupported {
		return invalid("resume support requires pause support")
	}
	audio, video, available := false, false, false
	for _, device := range c.Devices {
		if device.Available {
			available = true
			if device.Kind == DeviceKindAudioInput {
				audio = true
			}
			if device.Kind == DeviceKindVideoInput {
				video = true
			}
		}
	}
	if !available {
		return invalid("usable capability requires at least one available device")
	}
	if c.AudioAvailable != audio || c.VideoAvailable != video {
		return invalid("availability flags disagree with the device list")
	}
	if c.DefaultDeviceID != "" {
		found := false
		for _, device := range c.Devices {
			if device.ID == c.DefaultDeviceID {
				if !device.Default {
					return invalid("default device id references a non-default device")
				}
				found = true
			}
		}
		if !found {
			return invalid("default device id references no listed device")
		}
	}
	return nil
}

func validateIssues(issues []CapabilityIssue) error {
	invalid := func(message string) error {
		return NewError(ErrorInvalidRequest, OpCapabilities, false, OutcomeNotStarted, message, nil)
	}
	for i, issue := range issues {
		if strings.TrimSpace(issue.Code) == "" || containsControl(issue.Code) {
			return invalid("capability issue requires a safe code")
		}
		if strings.TrimSpace(issue.Message) == "" || len(issue.Message) > 256 || containsControl(issue.Message) {
			return invalid("capability issue requires a safe message")
		}
		if i > 0 && issues[i-1].Code > issue.Code {
			return invalid("capability issues are not in stable code order")
		}
	}
	return nil
}
