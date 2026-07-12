package capture

import (
	"testing"
)

func audioDevice(id string, isDefault, available bool) Device {
	return Device{ID: id, Name: "Synthetic " + id, Kind: DeviceKindAudioInput, Default: isDefault, Available: available}
}

func readyCapability() Capability {
	return Capability{
		Status:          CapabilityReady,
		AudioAvailable:  true,
		PauseSupported:  true,
		ResumeSupported: true,
		Devices:         []Device{audioDevice("dev-1", true, true)},
		DefaultDeviceID: "dev-1",
	}
}

func TestCapabilityValidation(t *testing.T) {
	degraded := readyCapability()
	degraded.Status = CapabilityDegraded
	degraded.Issues = []CapabilityIssue{{Code: "video_missing", Message: "no video input detected"}}

	tests := []struct {
		name    string
		mutate  func(*Capability)
		wantErr bool
	}{
		{"ready", func(*Capability) {}, false},
		{"unavailable claims nothing", func(c *Capability) { *c = Capability{Status: CapabilityUnavailable} }, false},
		{"unknown claims nothing", func(c *Capability) { *c = Capability{Status: CapabilityUnknown} }, false},
		{"degraded with issue", func(c *Capability) { *c = degraded }, false},
		{"invalid status", func(c *Capability) { c.Status = "sideways" }, true},
		{"unavailable with devices", func(c *Capability) {
			*c = Capability{Status: CapabilityUnavailable, Devices: []Device{audioDevice("dev-1", false, true)}}
		}, true},
		{"unavailable with audio flag", func(c *Capability) { *c = Capability{Status: CapabilityUnavailable, AudioAvailable: true} }, true},
		{"ready with issues", func(c *Capability) {
			c.Issues = []CapabilityIssue{{Code: "warn", Message: "warning"}}
		}, true},
		{"degraded without issues", func(c *Capability) { c.Status = CapabilityDegraded }, true},
		{"ready without available device", func(c *Capability) {
			c.Devices = []Device{audioDevice("dev-1", true, false)}
			c.AudioAvailable = false
		}, true},
		{"audio flag disagrees", func(c *Capability) { c.AudioAvailable = false }, true},
		{"video flag fabricated", func(c *Capability) { c.VideoAvailable = true }, true},
		{"resume without pause", func(c *Capability) { c.PauseSupported = false }, true},
		{"default id unlisted", func(c *Capability) { c.DefaultDeviceID = "dev-9" }, true},
		{"default id non-default device", func(c *Capability) {
			c.Devices = []Device{audioDevice("dev-1", false, true)}
		}, true},
		{"duplicate device ids", func(c *Capability) {
			c.Devices = []Device{audioDevice("dev-1", true, true), audioDevice("dev-1", false, true)}
		}, true},
		{"multiple defaults one kind", func(c *Capability) {
			c.Devices = []Device{audioDevice("dev-1", true, true), audioDevice("dev-2", true, true)}
		}, true},
		{"empty device id", func(c *Capability) { c.Devices = []Device{audioDevice("", true, true)} }, true},
		{"empty device name", func(c *Capability) {
			c.Devices = []Device{{ID: "dev-1", Kind: DeviceKindAudioInput, Default: true, Available: true}}
		}, true},
		{"unknown device kind", func(c *Capability) {
			c.Devices = []Device{{ID: "dev-1", Name: "Synthetic", Kind: "telepathy", Default: true, Available: true}}
		}, true},
		{"unstable device order", func(c *Capability) {
			c.Devices = []Device{audioDevice("dev-2", true, true), audioDevice("dev-1", false, true)}
			c.DefaultDeviceID = "dev-2"
		}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			capability := readyCapability()
			test.mutate(&capability)
			err := capability.Validate()
			if (err != nil) != test.wantErr {
				t.Fatalf("Validate() = %v, wantErr %v", err, test.wantErr)
			}
			if err != nil && CodeOf(err) != ErrorInvalidRequest {
				t.Fatalf("error code = %s, want invalid_request", CodeOf(err))
			}
		})
	}
}

func TestCapabilityCloneIsDefensive(t *testing.T) {
	original := readyCapability()
	clone := original.Clone()
	clone.Devices[0].ID = "mutated"
	clone.Devices = append(clone.Devices, audioDevice("dev-9", false, true))
	if original.Devices[0].ID != "dev-1" || len(original.Devices) != 1 {
		t.Fatalf("clone mutation leaked into original: %+v", original.Devices)
	}
}

func TestSortDevicesProducesStableValidOrder(t *testing.T) {
	devices := []Device{
		{ID: "b", Name: "B", Kind: DeviceKindVideoInput, Available: true},
		{ID: "a", Name: "A", Kind: DeviceKindAudioInput, Default: true, Available: true},
		{ID: "c", Name: "C", Kind: DeviceKindAudioInput, Available: true},
	}
	sorted := SortDevices(devices)
	if err := ValidateDevices(sorted); err != nil {
		t.Fatalf("sorted devices invalid: %v", err)
	}
	if sorted[0].ID != "a" || sorted[1].ID != "c" || sorted[2].ID != "b" {
		t.Fatalf("unexpected order: %+v", sorted)
	}
	if devices[0].ID != "b" {
		t.Fatal("SortDevices mutated its input")
	}
}
