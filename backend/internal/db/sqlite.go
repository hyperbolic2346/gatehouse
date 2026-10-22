package db

import (
	"database/sql"
	"fmt"
	"log"
	"math"
	"strings"

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
	// DisplayName is the name shown to other users (e.g. "held by ...").
	// Empty means fall back to Username; use Name() rather than reading it.
	DisplayName string `json:"display_name"`
	// HoldPriority ranks non-admin users for gate holds: a user cannot take
	// over or release a hold placed by someone with a higher priority.
	// Admins outrank everyone regardless of this value.
	HoldPriority int `json:"hold_priority"`
	// Cameras is the set of cameras this user is permitted to view
	// (admin-controlled). HiddenCameras is the subset the user has chosen to
	// hide from their own view (user-controlled); it is always a subset of
	// Cameras. Both are populated by the user lookup methods.
	Cameras       []string `json:"cameras"`
	HiddenCameras []string `json:"hidden_cameras"`
}

// Name returns the user's display name, falling back to the username.
func (u *User) Name() string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Username
}

// CanOperateGate reports whether the user may control the given gate. Gate 0
// is Wilson and gate 1 is Brigman; admins may control every gate.
func (u *User) CanOperateGate(gateID int) bool {
	if u.Role == "admin" {
		return true
	}
	switch gateID {
	case 0:
		return u.WilsonGate
	case 1:
		return u.BrigmanGate
	default:
		return false
	}
}

// HoldRank orders users for hold priority: a user cannot take over or
// release a hold placed by someone with a higher rank. Admins outrank everyone.
func (u *User) HoldRank() int {
	if u.Role == "admin" {
		return math.MaxInt
	}
	return u.HoldPriority
}

// HasLegacyPassword reports whether the stored hash is an unsalted md5 hash
// imported from the old gate site. It is upgraded to bcrypt on next login.
func (u *User) HasLegacyPassword() bool {
	return strings.HasPrefix(u.PasswordHash, LegacyMD5Prefix)
}

// LegacyMD5Prefix marks password hashes imported from the old gate site.
const LegacyMD5Prefix = "md5:"

// userColumns is the column list every user query selects, in scanUser order.
const userColumns = "id, username, password_hash, role, wilson_gate, brigman_gate, created_at, display_name, hold_priority"

type rowScanner interface {
	Scan(dest ...any) error
}

func scanUser(row rowScanner, u *User) error {
	return row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.WilsonGate, &u.BrigmanGate, &u.CreatedAt, &u.DisplayName, &u.HoldPriority)
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
	);

	CREATE TABLE IF NOT EXISTS activity_log (
		id      INTEGER PRIMARY KEY AUTOINCREMENT,
		at      TEXT NOT NULL,
		user_id INTEGER,
		actor   TEXT NOT NULL DEFAULT '',
		gate_id INTEGER,
		action  TEXT NOT NULL,
		detail  TEXT NOT NULL DEFAULT ''
	);

	CREATE INDEX IF NOT EXISTS activity_log_gate ON activity_log (gate_id, id);

	CREATE TABLE IF NOT EXISTS scheduled_holds (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		gate_id    INTEGER NOT NULL,
		user_id    INTEGER NOT NULL,
		owner      TEXT NOT NULL,
		start_at   TEXT NOT NULL,
		end_at     TEXT NOT NULL,
		note       TEXT NOT NULL DEFAULT '',
		status     TEXT NOT NULL,
		detail     TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL
	);

	CREATE INDEX IF NOT EXISTS scheduled_holds_status ON scheduled_holds (status, start_at);`

	if _, err := d.conn.Exec(schema); err != nil {
		return fmt.Errorf("create tables: %w", err)
	}

	// Columns added after the initial release; CREATE TABLE IF NOT EXISTS
	// does not add them to an existing database.
	if err := d.addColumnIfMissing("users", "display_name", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := d.addColumnIfMissing("users", "hold_priority", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}

	// Usernames are unique regardless of case. A database that already has
	// two usernames differing only by case cannot take the index; the API
	// still refuses new clashes, so warn rather than fail to start.
	if _, err := d.conn.Exec("CREATE UNIQUE INDEX IF NOT EXISTS users_username_nocase ON users (username COLLATE NOCASE)"); err != nil {
		log.Printf("Warning: usernames differing only by case exist; case-insensitive unique index not created: %v", err)
	}
	return nil
}

// addColumnIfMissing adds column to table unless it already exists.
func (d *DB) addColumnIfMissing(table, column, definition string) error {
	rows, err := d.conn.Query("SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return fmt.Errorf("inspect %s: %w", table, err)
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("inspect %s: %w", table, err)
	}
	if _, err := d.conn.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition)); err != nil {
		return fmt.Errorf("add %s.%s: %w", table, column, err)
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

// GetUserByUsername returns the user with the given username, ignoring case
// and surrounding spaces (phones capitalise the first letter and autocomplete
// can add a trailing space). If two existing users differ only by case, an
// exact match wins.
func (d *DB) GetUserByUsername(username string) (*User, error) {
	username = strings.TrimSpace(username)
	u := &User{}
	err := scanUser(d.conn.QueryRow(
		"SELECT "+userColumns+" FROM users WHERE username = ? COLLATE NOCASE ORDER BY username = ? DESC, id LIMIT 1",
		username, username,
	), u)
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
	err := scanUser(d.conn.QueryRow("SELECT "+userColumns+" FROM users WHERE id = ?", id), u)
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
	rows, err := d.conn.Query("SELECT " + userColumns + " FROM users ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := scanUser(rows, &u); err != nil {
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
		"INSERT INTO users (username, password_hash, role, wilson_gate, brigman_gate, display_name, hold_priority) VALUES (?, ?, ?, ?, ?, ?, ?)",
		u.Username, u.PasswordHash, u.Role, u.WilsonGate, u.BrigmanGate, u.DisplayName, u.HoldPriority,
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
		"UPDATE users SET username = ?, password_hash = ?, role = ?, wilson_gate = ?, brigman_gate = ?, display_name = ?, hold_priority = ? WHERE id = ?",
		u.Username, u.PasswordHash, u.Role, u.WilsonGate, u.BrigmanGate, u.DisplayName, u.HoldPriority, u.ID,
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
