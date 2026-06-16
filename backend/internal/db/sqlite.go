package db

import (
	"database/sql"
	"fmt"
	"log"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

// User represents a row in the users table.
type User struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"`
	WilsonGate   bool   `json:"wilson_gate"`
	BrigmanGate  bool   `json:"brigman_gate"`
	CreatedAt    string `json:"created_at"`
	// Cameras is the set of cameras this user is permitted to view
	// (admin-controlled). HiddenCameras is the subset the user has chosen to
	// hide from their own view (user-controlled); it is always a subset of
	// Cameras. Both are populated by the user lookup methods.
	Cameras       []string `json:"cameras"`
	HiddenCameras []string `json:"hidden_cameras"`
}

// DB wraps a sql.DB connection to the SQLite database.
type DB struct {
	conn           *sql.DB
	defaultCameras []string
}

// New opens the SQLite database at dbPath, runs migrations, and seeds a
// default admin user if the users table is empty. defaultCameras is the camera
// set granted to the seed admin and back-filled onto any pre-existing users
// the first time camera permissions are introduced.
func New(dbPath string, defaultCameras []string) (*DB, error) {
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Enable WAL mode for better concurrent read performance.
	if _, err := conn.Exec("PRAGMA journal_mode=WAL"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("set journal mode: %w", err)
	}

	d := &DB{conn: conn, defaultCameras: defaultCameras}

	if err := d.migrate(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	if err := d.seedAdmin(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("seed admin: %w", err)
	}

	if err := d.backfillCameras(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("backfill cameras: %w", err)
	}

	return d, nil
}

// Close closes the underlying database connection.
func (d *DB) Close() error {
	return d.conn.Close()
}

// migrate creates the schema if it does not already exist.
func (d *DB) migrate() error {
	const schema = `
	CREATE TABLE IF NOT EXISTS users (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		username      TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		role          TEXT NOT NULL DEFAULT 'user',
		wilson_gate   BOOLEAN NOT NULL DEFAULT 0,
		brigman_gate  BOOLEAN NOT NULL DEFAULT 0,
		created_at    DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS user_cameras (
		user_id INTEGER NOT NULL,
		camera  TEXT NOT NULL,
		PRIMARY KEY (user_id, camera)
	);

	CREATE TABLE IF NOT EXISTS user_hidden_cameras (
		user_id INTEGER NOT NULL,
		camera  TEXT NOT NULL,
		PRIMARY KEY (user_id, camera)
	);`

	if _, err := d.conn.Exec(schema); err != nil {
		return fmt.Errorf("create tables: %w", err)
	}
	return nil
}

// backfillCameras grants the default camera set to every existing user the
// first time camera permissions are introduced (i.e. when the user_cameras
// table is still empty but users already exist). This preserves the prior
// behaviour where all users could see all gate cameras.
func (d *DB) backfillCameras() error {
	if len(d.defaultCameras) == 0 {
		return nil
	}

	var camCount int
	if err := d.conn.QueryRow("SELECT COUNT(*) FROM user_cameras").Scan(&camCount); err != nil {
		return fmt.Errorf("count user_cameras: %w", err)
	}
	if camCount > 0 {
		return nil
	}

	rows, err := d.conn.Query("SELECT id FROM users")
	if err != nil {
		return fmt.Errorf("list user ids: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scan user id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate user ids: %w", err)
	}

	for _, id := range ids {
		if err := d.SetUserCameras(id, d.defaultCameras); err != nil {
			return fmt.Errorf("backfill cameras for user %d: %w", id, err)
		}
	}
	if len(ids) > 0 {
		log.Printf("Back-filled camera permissions for %d existing user(s): %v", len(ids), d.defaultCameras)
	}
	return nil
}

// seedAdmin creates the default admin user "knobby" when no users exist.
func (d *DB) seedAdmin() error {
	var count int
	if err := d.conn.QueryRow("SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		return fmt.Errorf("count users: %w", err)
	}
	if count > 0 {
		return nil
	}

	hash, err := bcrypt.GenerateFromPassword([]byte("changeme"), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash seed password: %w", err)
	}

	result, err := d.conn.Exec(
		"INSERT INTO users (username, password_hash, role, wilson_gate, brigman_gate) VALUES (?, ?, ?, ?, ?)",
		"knobby", string(hash), "admin", true, true,
	)
	if err != nil {
		return fmt.Errorf("insert seed admin: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("get seed admin id: %w", err)
	}
	if err := d.SetUserCameras(id, d.defaultCameras); err != nil {
		return fmt.Errorf("grant seed admin cameras: %w", err)
	}

	log.Println("Seeded default admin user: knobby")
	return nil
}

// GetUserByUsername returns the user with the given username.
func (d *DB) GetUserByUsername(username string) (*User, error) {
	u := &User{}
	err := d.conn.QueryRow(
		"SELECT id, username, password_hash, role, wilson_gate, brigman_gate, created_at FROM users WHERE username = ?",
		username,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.WilsonGate, &u.BrigmanGate, &u.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get user by username: %w", err)
	}
	if err := d.loadCameras(u); err != nil {
		return nil, err
	}
	return u, nil
}

// GetUserByID returns the user with the given id.
func (d *DB) GetUserByID(id int64) (*User, error) {
	u := &User{}
	err := d.conn.QueryRow(
		"SELECT id, username, password_hash, role, wilson_gate, brigman_gate, created_at FROM users WHERE id = ?",
		id,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.WilsonGate, &u.BrigmanGate, &u.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get user by id: %w", err)
	}
	if err := d.loadCameras(u); err != nil {
		return nil, err
	}
	return u, nil
}

// ListUsers returns all users in the database.
func (d *DB) ListUsers() ([]User, error) {
	rows, err := d.conn.Query(
		"SELECT id, username, password_hash, role, wilson_gate, brigman_gate, created_at FROM users ORDER BY id",
	)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.WilsonGate, &u.BrigmanGate, &u.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate users: %w", err)
	}
	for i := range users {
		if err := d.loadCameras(&users[i]); err != nil {
			return nil, err
		}
	}
	return users, nil
}

// CreateUser inserts a new user into the database. The ID field of u is
// updated with the newly assigned id on success.
func (d *DB) CreateUser(u *User) error {
	result, err := d.conn.Exec(
		"INSERT INTO users (username, password_hash, role, wilson_gate, brigman_gate) VALUES (?, ?, ?, ?, ?)",
		u.Username, u.PasswordHash, u.Role, u.WilsonGate, u.BrigmanGate,
	)
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("get last insert id: %w", err)
	}
	u.ID = id

	if err := d.SetUserCameras(u.ID, u.Cameras); err != nil {
		return fmt.Errorf("set cameras: %w", err)
	}
	return nil
}

// UpdateUser updates an existing user identified by u.ID.
func (d *DB) UpdateUser(u *User) error {
	_, err := d.conn.Exec(
		"UPDATE users SET username = ?, password_hash = ?, role = ?, wilson_gate = ?, brigman_gate = ? WHERE id = ?",
		u.Username, u.PasswordHash, u.Role, u.WilsonGate, u.BrigmanGate, u.ID,
	)
	if err != nil {
		return fmt.Errorf("update user: %w", err)
	}
	return nil
}

// DeleteUser removes the user with the given id along with its camera grants
// and hidden-camera preferences.
func (d *DB) DeleteUser(id int64) error {
	tx, err := d.conn.Begin()
	if err != nil {
		return fmt.Errorf("delete user: begin tx: %w", err)
	}
	defer tx.Rollback()

	for _, stmt := range []string{
		"DELETE FROM user_hidden_cameras WHERE user_id = ?",
		"DELETE FROM user_cameras WHERE user_id = ?",
		"DELETE FROM users WHERE id = ?",
	} {
		if _, err := tx.Exec(stmt, id); err != nil {
			return fmt.Errorf("delete user: %w", err)
		}
	}
	return tx.Commit()
}

// loadCameras populates the Cameras and HiddenCameras fields of u from the
// join tables. Both are guaranteed non-nil so they serialise as [] not null.
func (d *DB) loadCameras(u *User) error {
	cams, err := d.GetUserCameras(u.ID)
	if err != nil {
		return err
	}
	hidden, err := d.GetUserHiddenCameras(u.ID)
	if err != nil {
		return err
	}
	u.Cameras = cams
	u.HiddenCameras = hidden
	return nil
}

// GetUserCameras returns the sorted list of cameras the user is permitted to
// view. The result is always non-nil.
func (d *DB) GetUserCameras(userID int64) ([]string, error) {
	return d.queryCameras("SELECT camera FROM user_cameras WHERE user_id = ? ORDER BY camera", userID)
}

// GetUserHiddenCameras returns the sorted list of cameras the user has hidden
// from their own view. The result is always non-nil.
func (d *DB) GetUserHiddenCameras(userID int64) ([]string, error) {
	return d.queryCameras("SELECT camera FROM user_hidden_cameras WHERE user_id = ? ORDER BY camera", userID)
}

func (d *DB) queryCameras(query string, userID int64) ([]string, error) {
	rows, err := d.conn.Query(query, userID)
	if err != nil {
		return nil, fmt.Errorf("query cameras: %w", err)
	}
	defer rows.Close()

	cameras := []string{}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, fmt.Errorf("scan camera: %w", err)
		}
		cameras = append(cameras, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cameras: %w", err)
	}
	return cameras, nil
}

// SetUserCameras replaces the user's permitted cameras with the given set.
// Any hidden-camera preferences that are no longer permitted are dropped so
// the hidden set stays a subset of the permitted set.
func (d *DB) SetUserCameras(userID int64, cameras []string) error {
	return d.replaceCameras(userID, cameras, "user_cameras", true)
}

// SetUserHiddenCameras replaces the user's hidden cameras with the given set.
func (d *DB) SetUserHiddenCameras(userID int64, cameras []string) error {
	return d.replaceCameras(userID, cameras, "user_hidden_cameras", false)
}

// replaceCameras atomically replaces all rows for userID in the given table
// with the supplied camera set. When pruneHidden is true, hidden cameras that
// are not in the new permitted set are also removed.
func (d *DB) replaceCameras(userID int64, cameras []string, table string, pruneHidden bool) error {
	tx, err := d.conn.Begin()
	if err != nil {
		return fmt.Errorf("set cameras: begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM "+table+" WHERE user_id = ?", userID); err != nil {
		return fmt.Errorf("set cameras: clear %s: %w", table, err)
	}

	seen := make(map[string]bool, len(cameras))
	for _, c := range cameras {
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		if _, err := tx.Exec("INSERT INTO "+table+" (user_id, camera) VALUES (?, ?)", userID, c); err != nil {
			return fmt.Errorf("set cameras: insert into %s: %w", table, err)
		}
	}

	if pruneHidden {
		if _, err := tx.Exec(
			"DELETE FROM user_hidden_cameras WHERE user_id = ? AND camera NOT IN (SELECT camera FROM user_cameras WHERE user_id = ?)",
			userID, userID,
		); err != nil {
			return fmt.Errorf("set cameras: prune hidden: %w", err)
		}
	}

	return tx.Commit()
}
