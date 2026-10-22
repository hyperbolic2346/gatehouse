package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestUsernameLookupIgnoresCaseAndSpaces(t *testing.T) {
	d := newTestDB(t, nil)
	if err := d.CreateUser(&User{Username: "nana", PasswordHash: "x", Role: "user"}); err != nil {
		t.Fatal(err)
	}
	for _, typed := range []string{"nana", "Nana", "NANA", " Nana ", "nana\t"} {
		u, err := d.GetUserByUsername(typed)
		if err != nil || u.Username != "nana" {
			t.Errorf("GetUserByUsername(%q) = %v, %v; want nana", typed, u, err)
		}
	}
	if _, err := d.GetUserByUsername("nan"); err == nil {
		t.Error("a different name must not match")
	}
}

func TestUsernamesUniqueRegardlessOfCase(t *testing.T) {
	d := newTestDB(t, nil)
	if err := d.CreateUser(&User{Username: "nana", PasswordHash: "x", Role: "user"}); err != nil {
		t.Fatal(err)
	}
	if err := d.CreateUser(&User{Username: "Nana", PasswordHash: "x", Role: "user"}); err == nil {
		t.Error("creating Nana alongside nana should fail at the database")
	}
}

func TestExistingCaseDuplicatesStillStartAndPreferExactMatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dupes.db")
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`CREATE TABLE users (
		id INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL, role TEXT NOT NULL DEFAULT 'user',
		wilson_gate BOOLEAN NOT NULL DEFAULT 0, brigman_gate BOOLEAN NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP);
		INSERT INTO users (username, password_hash) VALUES ('bob', 'x'), ('Bob', 'y');`); err != nil {
		t.Fatal(err)
	}
	conn.Close()

	d, err := New(path, nil)
	if err != nil {
		t.Fatalf("New with case duplicates must still start: %v", err)
	}
	defer d.Close()

	if u, _ := d.GetUserByUsername("Bob"); u == nil || u.PasswordHash != "y" {
		t.Errorf("exact match Bob = %+v, want the Bob row", u)
	}
	if u, _ := d.GetUserByUsername("bob"); u == nil || u.PasswordHash != "x" {
		t.Errorf("exact match bob = %+v, want the bob row", u)
	}
	if u, _ := d.GetUserByUsername("BOB"); u == nil || u.PasswordHash != "x" {
		t.Errorf("no exact match: BOB = %+v, want the oldest (bob)", u)
	}
}
