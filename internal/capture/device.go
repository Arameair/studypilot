package capture

import "strings"

// DeviceKind classifies a capture input device.
type DeviceKind string

const (
	DeviceKindAudioInput DeviceKind = "audio_input"
	DeviceKindVideoInput DeviceKind = "video_input"
)

func (k DeviceKind) Valid() bool {
	return k == DeviceKindAudioInput || k == DeviceKindVideoInput
}

// Device is a UI-neutral description of one capture input. It never carries
// platform-specific handles; implementations keep those private and translate
// to this representation at the boundary.
type Device struct {
	ID        string
	Name      string
	Kind      DeviceKind
	Default   bool
	Available bool
}

// Validate rejects devices that could not be addressed or displayed safely.
func (d Device) Validate() error {
	invalid := func(message string) error {
		return NewError(ErrorInvalidRequest, "", false, OutcomeNotStarted, message, nil)
	}
	if strings.TrimSpace(d.ID) == "" || len(d.ID) > maxIDLength || containsControl(d.ID) {
		return invalid("device id is empty or unsafe")
	}
	if strings.TrimSpace(d.Name) == "" || len(d.Name) > 256 || containsControl(d.Name) {
		return invalid("device name is empty or unsafe")
	}
	if !d.Kind.Valid() {
		return invalid("unknown device kind")
	}
	return nil
}

// ValidateDevices checks each device, uniqueness of IDs, at most one default
// per kind, and the stable-ordering contract: devices must be sorted by kind
// then ID so every implementation reports them deterministically.
func ValidateDevices(devices []Device) error {
	invalid := func(message string) error {
		return NewError(ErrorInvalidRequest, "", false, OutcomeNotStarted, message, nil)
	}
	ids := make(map[string]bool, len(devices))
	defaults := make(map[DeviceKind]bool, 2)
	for i, device := range devices {
		if err := device.Validate(); err != nil {
			return err
		}
		if ids[device.ID] {
			return invalid("duplicate device id")
		}
		ids[device.ID] = true
		if device.Default {
			if defaults[device.Kind] {
				return invalid("multiple default devices for one kind")
			}
			defaults[device.Kind] = true
		}
		if i > 0 {
			previous := devices[i-1]
			if previous.Kind > device.Kind || (previous.Kind == device.Kind && previous.ID >= device.ID) {
				return invalid("devices are not in stable kind then id order")
			}
		}
	}
	return nil
}

// SortDevices returns a copy of devices in the stable order ValidateDevices
// requires. The input slice is never modified.
func SortDevices(devices []Device) []Device {
	sorted := append([]Device(nil), devices...)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0; j-- {
			a, b := sorted[j-1], sorted[j]
			if a.Kind < b.Kind || (a.Kind == b.Kind && a.ID <= b.ID) {
				break
			}
			sorted[j-1], sorted[j] = b, a
		}
	}
	return sorted
}
