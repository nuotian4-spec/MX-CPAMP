package kilo

import "context"

// DeviceProfile belongs to one credential and survives egress reassignment.
type DeviceProfile struct {
	ID        string `json:"id"`
	MachineID string `json:"machine_id"`
}

// DeviceProfileStore persists identities before an egress state can reference them.
type DeviceProfileStore func(context.Context, string, int) ([]DeviceProfile, error)
