// Package commandsdb parses the embedded commandsDB.json catalogue and exposes
// the command IDs the scan layer reads from a card. The catalogue's command IDs
// are the RollCall wire IDs and are used unchanged.
package commandsdb

import (
	"encoding/json"
	"fmt"

	demeter "github.com/Xeue/Demeter"
)

// Spigots is the number of spigots read per card. The legacy app reads all 16
// regardless of how many are inputs (the inOnly filter is commented out in
// main.ts checkCard), so we do the same.
const Spigots = 16

// DB is the parsed command catalogue: card-level groups and per-spigot groups.
type DB struct {
	Card   []Group `json:"card"`
	Spigot []Group `json:"spigot"`
}

// Group is a named set of commands shown together in the UI.
type Group struct {
	Name     string    `json:"name"`
	Commands []Command `json:"commands"`
}

// Command is one catalogue entry. Only the fields the backend needs are typed;
// the rest (default, options, depends, restart) ride along in the raw JSON that
// is injected into the page.
type Command struct {
	Command   uint32 `json:"command"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Increment int    `json:"increment,omitempty"`
	Take      uint32 `json:"take,omitempty"`
	InOnly    bool   `json:"inOnly,omitempty"`
	Shuffle   bool   `json:"shuffle,omitempty"`
	Restart   bool   `json:"restart,omitempty"` // applying this needs a card reboot
}

// Load parses the embedded catalogue.
func Load() (*DB, error) {
	var db DB
	if err := json.Unmarshal(demeter.CommandsJSON, &db); err != nil {
		return nil, fmt.Errorf("commandsdb: parse commandsDB.json: %w", err)
	}
	if len(db.Card) == 0 || len(db.Spigot) == 0 {
		return nil, fmt.Errorf("commandsdb: catalogue is empty (card=%d spigot=%d)", len(db.Card), len(db.Spigot))
	}
	return &db, nil
}

// RawJSON returns the catalogue bytes verbatim, for injection into the page.
func RawJSON() []byte { return demeter.CommandsJSON }

// RestartNames returns the command id -> display name for every command flagged
// restart:true (applying it needs a card reboot). Restart-flagged commands are
// card-level with no increment, so their base ids are used directly.
func (db *DB) RestartNames() map[uint32]string {
	out := map[uint32]string{}
	for _, groups := range [][]Group{db.Card, db.Spigot} {
		for _, g := range groups {
			for _, c := range g.Commands {
				if c.Restart {
					out[c.Command] = c.Name
				}
			}
		}
	}
	return out
}

// CardScanIDs returns the full ordered list of command IDs a single card read
// covers: every card command, then for spigot index 0..15 every spigot command
// offset by increment*index (or the bare command when it has no increment).
// This mirrors main.ts checkCard's list build exactly (the inOnly filter stays
// disabled, so all spigots are read).
func (db *DB) CardScanIDs() []uint32 {
	ids := make([]uint32, 0, 298)
	for _, g := range db.Card {
		for _, c := range g.Commands {
			ids = append(ids, c.Command)
		}
	}
	for index := 0; index < Spigots; index++ {
		for _, g := range db.Spigot {
			for _, c := range g.Commands {
				if c.Increment != 0 {
					ids = append(ids, c.Command+uint32(c.Increment*index))
				} else {
					ids = append(ids, c.Command)
				}
			}
		}
	}
	return ids
}
