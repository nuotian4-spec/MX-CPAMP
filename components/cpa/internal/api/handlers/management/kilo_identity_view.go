package management

import (
	"sort"
	"strconv"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/kilo"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

type kiloClientIdentityDTO struct {
	State    string                 `json:"state"`
	Profiles []kiloClientProfileDTO `json:"profiles"`
}

type kiloClientProfileDTO struct {
	Ref   string               `json:"ref"`
	Exits *[]kiloClientExitDTO `json:"exits,omitempty"`
}

type kiloClientExitDTO struct {
	Kind string `json:"kind"`
	IP   string `json:"ip,omitempty"`
}

func kiloClientIdentityView(a *coreauth.Auth, bindings map[string]string, available bool) *kiloClientIdentityDTO {
	view := &kiloClientIdentityDTO{State: "pending", Profiles: []kiloClientProfileDTO{}}
	profiles, err := coreauth.KiloDeviceProfiles(a)
	if err != nil {
		view.State = "invalid"
		return view
	}
	if len(profiles) == 0 {
		return view
	}
	view.State = "persisted"
	indices := make(map[string]int, len(profiles))
	for index, profile := range profiles {
		view.Profiles = append(view.Profiles, kiloClientProfileDTO{Ref: "profile-" + strconv.Itoa(index+1)})
		indices[profile.ID] = index
	}
	if !available {
		return view
	}
	keys := make([]string, 0, len(bindings))
	for exit, id := range bindings {
		if _, ok := indices[id]; !ok {
			// Independently published snapshots may briefly disagree.
			return view
		}
		keys = append(keys, exit)
	}
	for index := range view.Profiles {
		exits := []kiloClientExitDTO{}
		view.Profiles[index].Exits = &exits
	}
	sort.Strings(keys)
	for _, key := range keys {
		exit := kiloClientExitDTO{Kind: key}
		switch key {
		case "direct", "legacy":
		default:
			ip, ok := kilo.FixedPublicIP(key)
			if !ok || ip != key {
				continue
			}
			exit = kiloClientExitDTO{Kind: "ip", IP: ip}
		}
		profile := &view.Profiles[indices[bindings[key]]]
		*profile.Exits = append(*profile.Exits, exit)
	}
	return view
}
