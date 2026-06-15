package model

// Frames is the top-level frames map, keyed by frame IP. Matches frames.json.
type Frames map[string]*Frame

// Groups is the top-level groups map, keyed by group name. Matches groups.json.
type Groups map[string]*Group

// Frame mirrors the legacy Frame type (main.ts ~89). JSON tags match the
// on-disk frames.json so legacy data loads unchanged and the front-end needs no
// data-shape change.
type Frame struct {
	Number  string           `json:"number"`
	Name    string           `json:"name"`
	IP      string           `json:"ip"`
	Enabled bool             `json:"enabled"`
	Scan    bool             `json:"scan"`
	Group   string           `json:"group"`
	Slots   map[string]*Slot `json:"slots"`
	Done    bool             `json:"done"`
	Offline bool             `json:"offline,omitempty"`
	Type    string           `json:"type"`
	// AutoReboot is the per-frame override of the global auto-reboot default:
	// "" = inherit, "on" = force on, "off" = force off.
	AutoReboot string `json:"autoReboot,omitempty"`
}

// Slot mirrors the legacy Slot type (main.ts ~102). The prefered/active/group
// maps are keyed by command id (string).
type Slot struct {
	Enabled bool    `json:"enabled"`
	IPA     *string `json:"ipa,omitempty"`
	IPB     *string `json:"ipb,omitempty"`
	IPAUp   string  `json:"ipaup,omitempty"`
	IPBUp   string  `json:"ipbup,omitempty"`
	SFP1    string  `json:"sfp1,omitempty"`
	SFP2    string  `json:"sfp2,omitempty"`
	Offline bool    `json:"offline"`
	Staged  bool    `json:"staged,omitempty"` // pre-configured before the card was ever discovered
	// RebootNeeded is set when a restart-required change was blasted to this card
	// and has not yet been applied; RebootReasons explains why (for the UI tooltip).
	RebootNeeded  bool     `json:"rebootNeeded,omitempty"`
	RebootReasons []string `json:"rebootReasons,omitempty"`
	// Failed maps a command id to a reason when a blasted SET did not take effect
	// after the immediate verify-and-retry (the device echo never matched). The
	// background poll loop keeps retrying; this just surfaces a stuck control.
	Failed   map[string]string        `json:"failed,omitempty"`
	Prefered map[string]FramePrefered `json:"prefered"`
	Active   map[string]Value         `json:"active"`
	Group    map[string]FrameGroup    `json:"group"`
	Ins      int                      `json:"ins"`
	Outs     int                      `json:"outs"`
}

// NewSlot returns a slot with its maps initialised (so they serialise as {} and
// are safe to index, matching the legacy initialisation).
func NewSlot() *Slot {
	return &Slot{
		Enabled:  true,
		Prefered: map[string]FramePrefered{},
		Active:   map[string]Value{},
		Group:    map[string]FrameGroup{},
	}
}

// FramePrefered is a per-card override the operator set (main.ts ~125).
type FramePrefered struct {
	Value    Value  `json:"value"`
	Enabled  bool   `json:"enabled"`
	Type     string `json:"type"`
	DataType string `json:"dataType,omitempty"`
	Take     Num    `json:"take,omitempty"`
}

// FrameGroup is a computed group command for a slot (main.ts ~118).
type FrameGroup struct {
	Value   Value  `json:"value"`
	Type    string `json:"type"`
	Enabled bool   `json:"enabled"`
	Take    Num    `json:"take"`
}

// Group is a named, enable-able set of command definitions (main.ts ~60).
type Group struct {
	Name     string                `json:"name"`
	Enabled  bool                  `json:"enabled"`
	Commands map[string]CommandDef `json:"commands"`
}

// CommandDef is one command within a group (main.ts ~66).
type CommandDef struct {
	Value     Value  `json:"value"`
	Enabled   bool   `json:"enabled"`
	Type      string `json:"type"`
	DataType  string `json:"dataType"`
	Increment Num    `json:"increment"`
	Take      Num    `json:"take"`
}
