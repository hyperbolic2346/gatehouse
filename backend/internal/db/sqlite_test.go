package db

import (
	"path/filepath"
	"reflect"
	"testing"
)

// newTestDB opens a fresh database in a temp dir with the given default cameras.
func newTestDB(t *testing.T, defaultCameras []string) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	d, err := New(path, defaultCameras)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestSeedAdminGetsDefaultCameras(t *testing.T) {
	d := newTestDB(t, []string{"gate", "gate-rear"})

	admin, err := d.GetUserByUsername("knobby")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if want := []string{"gate", "gate-rear"}; !reflect.DeepEqual(admin.Cameras, want) {
		t.Errorf("seed admin cameras = %v, want %v", admin.Cameras, want)
	}
}

func TestSetUserCamerasReplacesAndSorts(t *testing.T) {
	d := newTestDB(t, []string{"gate"})

	u := &User{Username: "alice", PasswordHash: "x", Role: "user", Cameras: []string{"gate"}}
	if err := d.CreateUser(u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Replace with an unsorted set containing a duplicate.
	if err := d.SetUserCameras(u.ID, []string{"driveway", "gate", "gate"}); err != nil {
		t.Fatalf("SetUserCameras: %v", err)
	}
	got, err := d.GetUserCameras(u.ID)
	if err != nil {
		t.Fatalf("GetUserCameras: %v", err)
	}
	if want := []string{"driveway", "gate"}; !reflect.DeepEqual(got, want) {
		t.Errorf("cameras = %v, want %v (sorted, deduped)", got, want)
	}
}

func TestHiddenCamerasPrunedWhenPermissionRemoved(t *testing.T) {
	d := newTestDB(t, []string{"gate"})
	u := &User{Username: "bob", PasswordHash: "x", Role: "user", Cameras: []string{"gate", "driveway"}}
	if err := d.CreateUser(u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if err := d.SetUserHiddenCameras(u.ID, []string{"gate", "driveway"}); err != nil {
		t.Fatalf("SetUserHiddenCameras: %v", err)
	}

	// Revoke "driveway"; the hidden entry for it must be pruned so hidden
	// stays a subset of permitted.
	if err := d.SetUserCameras(u.ID, []string{"gate"}); err != nil {
		t.Fatalf("SetUserCameras: %v", err)
	}
	hidden, err := d.GetUserHiddenCameras(u.ID)
	if err != nil {
		t.Fatalf("GetUserHiddenCameras: %v", err)
	}
	if want := []string{"gate"}; !reflect.DeepEqual(hidden, want) {
		t.Errorf("hidden = %v, want %v (driveway pruned)", hidden, want)
	}
}

func TestDeleteUserCleansCameraRows(t *testing.T) {
	d := newTestDB(t, []string{"gate"})
	u := &User{Username: "carol", PasswordHash: "x", Role: "user", Cameras: []string{"gate", "driveway"}}
	if err := d.CreateUser(u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := d.SetUserHiddenCameras(u.ID, []string{"driveway"}); err != nil {
		t.Fatalf("SetUserHiddenCameras: %v", err)
	}

	if err := d.DeleteUser(u.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	cams, err := d.GetUserCameras(u.ID)
	if err != nil {
		t.Fatalf("GetUserCameras: %v", err)
	}
	if len(cams) != 0 {
		t.Errorf("after delete, cameras = %v, want empty", cams)
	}
	hidden, err := d.GetUserHiddenCameras(u.ID)
	if err != nil {
		t.Fatalf("GetUserHiddenCameras: %v", err)
	}
	if len(hidden) != 0 {
		t.Errorf("after delete, hidden = %v, want empty", hidden)
	}
}

func TestGetUserCamerasEmptyIsNonNil(t *testing.T) {
	d := newTestDB(t, nil)
	u := &User{Username: "dave", PasswordHash: "x", Role: "user"}
	if err := d.CreateUser(u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	cams, err := d.GetUserCameras(u.ID)
	if err != nil {
		t.Fatalf("GetUserCameras: %v", err)
	}
	if cams == nil {
		t.Error("GetUserCameras returned nil, want non-nil empty slice")
	}
}
