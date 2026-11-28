package project

import (
	"fmt"
	"sort"
)

// AssetPlacement describes the destination package for one named asset group.
type AssetPlacement struct {
	Group   string
	Package string
}

// PlaceGroups resolves groups to deterministic package names. Unlisted groups stay in main.
func (d *AssetDelivery) PlaceGroups(groups []string) ([]AssetPlacement, error) {
	if d == nil {
		out := make([]AssetPlacement, len(groups))
		for i, g := range groups {
			out[i] = AssetPlacement{Group: g, Package: "main"}
		}
		return out, nil
	}
	claimed := map[string]string{}
	for _, g := range d.Main {
		claimed[g] = "main"
	}
	for n, gs := range d.Subpackages {
		for _, g := range gs {
			claimed[g] = "subpackage/" + n
		}
	}
	for _, g := range d.Remote {
		claimed[g] = "remote"
	}
	out := make([]AssetPlacement, len(groups))
	for i, g := range groups {
		if g == "" {
			return nil, fmt.Errorf("project: empty asset group")
		}
		p := claimed[g]
		if p == "" {
			p = "main"
		}
		out[i] = AssetPlacement{Group: g, Package: p}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Group < out[j].Group })
	return out, nil
}
