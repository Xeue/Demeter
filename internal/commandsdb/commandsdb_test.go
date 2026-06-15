package commandsdb

import "testing"

func TestLoadAndCardScanIDs(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Card commands + 16 spigots * spigot commands. The catalogue today is
	// 58 card + 15 spigot-per-index = 58 + 16*15 = 298. Lock the count so a
	// catalogue edit that changes the read shape is a deliberate, visible change.
	cardCount := 0
	for _, g := range db.Card {
		cardCount += len(g.Commands)
	}
	spigotPer := 0
	for _, g := range db.Spigot {
		spigotPer += len(g.Commands)
	}
	wantTotal := cardCount + Spigots*spigotPer
	ids := db.CardScanIDs()
	if len(ids) != wantTotal {
		t.Fatalf("CardScanIDs len = %d, want %d (card=%d spigotPer=%d)", len(ids), wantTotal, cardCount, spigotPer)
	}
	if cardCount != 58 || spigotPer != 15 || wantTotal != 298 {
		t.Errorf("catalogue shape changed: card=%d spigotPer=%d total=%d (was 58/15/298)", cardCount, spigotPer, wantTotal)
	}

	// Spot-check the spigot increment math: a command with increment 300 must
	// appear at base and base+300*15 across the 16 spigots.
	var base, inc uint32
	for _, g := range db.Spigot {
		for _, c := range g.Commands {
			if c.Increment == 300 {
				base, inc = c.Command, uint32(c.Increment)
				break
			}
		}
		if base != 0 {
			break
		}
	}
	if base == 0 {
		t.Fatal("expected at least one spigot command with increment 300")
	}
	want := base + inc*15
	found := false
	for _, id := range ids {
		if id == want {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected spigot id %d (base %d + 300*15) in scan ids", want, base)
	}
}
