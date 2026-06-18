package manager

import (
	"context"
	"testing"
	"time"

	"github.com/Xeue/Demeter/internal/commandsdb"
	"github.com/Xeue/Demeter/internal/device"
	"github.com/Xeue/Demeter/internal/model"
	"github.com/Xeue/Demeter/internal/pool"
	"github.com/Xeue/Demeter/internal/scan"
)

type noopPersister struct{}

func (noopPersister) SaveFrames(model.Frames, bool) {}
func (noopPersister) SaveGroups(model.Groups, bool) {}

type noopBroadcaster struct{}

func (noopBroadcaster) BroadcastFrames(model.Frames) {}
func (noopBroadcaster) BroadcastGroups(model.Groups) {}

type noopScanEvents struct{}

func (noopScanEvents) FrameStatus(string, string, bool)                   {}
func (noopScanEvents) SlotInfo(string, *model.Frame, string, *model.Slot) {}
func (noopScanEvents) SlotInfoBatch(string, *model.Frame, []string)       {}
func (noopScanEvents) FrameError(string, string)                          {}

func testManager(t *testing.T, frames model.Frames, groups model.Groups) (*Manager, context.CancelFunc) {
	t.Helper()
	db, err := commandsdb.Load()
	if err != nil {
		t.Fatal(err)
	}
	sc := &scan.Scanner{DB: db, Pool: pool.New(4), Events: noopScanEvents{}}
	ctx, cancel := context.WithCancel(context.Background())
	m := New(ctx, sc, device.NewFakeDialer(), noopPersister{}, noopBroadcaster{}, frames, groups, time.Hour, AutoRebootOptions{})
	return m, cancel
}

// TestImportDataCreatesFramesAndGroups: a granular import creates a new frame
// (with its card's prefered overrides) and upserts a group, but never turns
// blasting on.
func TestImportDataCreatesFramesAndGroups(t *testing.T) {
	m, cancel := testManager(t, model.Frames{}, model.Groups{})
	defer cancel()

	imp := model.Frames{
		"10.0.0.1": {
			IP: "10.0.0.1", Number: "7", Name: "Studio A", Group: "g1", Type: "ucp",
			Enabled: true, // import requests blasting on - must be ignored
			Scan:    false,
			Slots: map[string]*model.Slot{
				"01": {
					Enabled: true,
					Prefered: map[string]model.FramePrefered{
						"4101": {Value: model.StrVal("10.40.0.20"), Enabled: true, Type: "smartip"},
					},
					Active: map[string]model.Value{},
					Group:  map[string]model.FrameGroup{},
				},
			},
		},
	}
	groups := model.Groups{"g1": {Name: "g1", Enabled: true, Commands: map[string]model.CommandDef{}}}

	m.ImportData(imp, groups)

	f := m.FramesSnapshot()["10.0.0.1"]
	if f == nil {
		t.Fatal("frame not created by import")
	}
	if f.Enabled {
		t.Error("import must not turn blasting on")
	}
	if f.Group != "g1" {
		t.Errorf("group = %q, want g1", f.Group)
	}
	sl := f.Slots["01"]
	if sl == nil || sl.Prefered["4101"].Value.String() != "10.40.0.20" {
		t.Errorf("card prefered override not imported: %+v", sl)
	}
	if _, ok := m.GroupsSnapshot()["g1"]; !ok {
		t.Error("group g1 not imported")
	}
}

// TestImportDataLeavesOthersUntouched: importing one frame/group must not delete
// or alter frames/groups that are not in the import (merge, not replace).
func TestImportDataLeavesOthersUntouched(t *testing.T) {
	existing := model.Frames{
		"10.0.0.9": {IP: "10.0.0.9", Number: "9", Group: "keep", Scan: false, Slots: map[string]*model.Slot{}},
	}
	m, cancel := testManager(t, existing, model.Groups{"keep": {Name: "keep"}})
	defer cancel()

	m.ImportData(model.Frames{
		"10.0.0.2": {IP: "10.0.0.2", Number: "2", Scan: false, Slots: map[string]*model.Slot{}},
	}, model.Groups{"new": {Name: "new"}})

	frames := m.FramesSnapshot()
	if frames["10.0.0.9"] == nil {
		t.Error("pre-existing frame was dropped by a granular import")
	}
	if frames["10.0.0.2"] == nil {
		t.Error("imported frame missing")
	}
	groups := m.GroupsSnapshot()
	if _, ok := groups["keep"]; !ok {
		t.Error("pre-existing group was dropped by a granular import")
	}
	if _, ok := groups["new"]; !ok {
		t.Error("imported group missing")
	}
}

// TestExportSnapshotCleansRuntime: export keeps config (prefered, group, staged)
// but clears scan-derived runtime fields so the file is portable.
func TestExportSnapshotCleansRuntime(t *testing.T) {
	frames := model.Frames{
		"10.0.0.1": {
			IP: "10.0.0.1", Number: "7", Group: "g1", Offline: true,
			Slots: map[string]*model.Slot{
				"01": {
					Enabled: true, Offline: true, RebootNeeded: true, RebootReasons: []string{"x"},
					Active:   map[string]model.Value{"4101": model.StrVal("1.2.3.4")},
					Group:    map[string]model.FrameGroup{"4108": {Value: model.IntVal(1)}},
					Prefered: map[string]model.FramePrefered{"4101": {Value: model.StrVal("9.9.9.9"), Enabled: true, Type: "smartip"}},
				},
			},
		},
	}
	m, cancel := testManager(t, frames, model.Groups{})
	defer cancel()

	ef, _ := m.ExportSnapshot()
	sl := ef["10.0.0.1"].Slots["01"]
	if len(sl.Active) != 0 {
		t.Error("export should clear active values")
	}
	if len(sl.Group) != 0 {
		t.Error("export should clear computed group commands")
	}
	if sl.Offline || sl.RebootNeeded {
		t.Error("export should clear runtime flags")
	}
	if sl.Prefered["4101"].Value.String() != "9.9.9.9" {
		t.Error("export must keep prefered overrides")
	}
}
