package model

import "encoding/json"

// CloneFrame returns a deep copy of a frame (via JSON round-trip - the custom
// Value (un)marshalling round-trips exactly).
func CloneFrame(f *Frame) *Frame {
	if f == nil {
		return nil
	}
	b, _ := json.Marshal(f)
	var out Frame
	_ = json.Unmarshal(b, &out)
	return &out
}

// CloneFrames deep-copies a frames map.
func CloneFrames(in map[string]*Frame) Frames {
	out := make(Frames, len(in))
	for ip, f := range in {
		out[ip] = CloneFrame(f)
	}
	return out
}

// CloneGroup returns a deep copy of a group.
func CloneGroup(g *Group) *Group {
	if g == nil {
		return nil
	}
	b, _ := json.Marshal(g)
	var out Group
	_ = json.Unmarshal(b, &out)
	return &out
}

// CloneGroups deep-copies a groups map.
func CloneGroups(in map[string]*Group) Groups {
	out := make(Groups, len(in))
	for name, g := range in {
		out[name] = CloneGroup(g)
	}
	return out
}
