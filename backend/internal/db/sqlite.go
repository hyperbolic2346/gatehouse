package db

import (
	"database/sql"
	"fmt"
	"log"

	_ "modernc.org/sqlite"
	"golang.org/x/crypto/bcrypt"
)

// User represents a row in the users table.
type User struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	PasswordHash string `json:"-"`
	Role        string `json:"role"`
	WilsonGate  bool   `json:"wilson_gate"`
	BrigmanGate bool   `json:"brigman_gate"`
	CreatedAt   string `json:"created_at"`
}

// DB wraps a sql.DB connection to the SQLite database.
type DB struct {
	conn *sql.DB
}

// New opens the SQLite database at dbPath, runs migrations, and seeds a
// default admin user if the users table is empty.
func New(dbPath string) (*DB, error) {
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Enable WAL mode for better concurrent read performance.
	if _, err := conn.Exec("PRAGMA journal_mode=WAL"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("set journal mode: %w", err)
	}

	d := &DB{conn: conn}

	if err := d.migrate(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	if err := d.seedAdmin(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("seed admin: %w", err)
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
	);`

	if _, err := d.conn.Exec(schema); err != nil {
		return fmt.Errorf("create users table: %w", err)
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

	_, err = d.conn.Exec(
		"INSERT INTO users (username, password_hash, role, wilson_gate, brigman_gate) VALUES (?, ?, ?, ?, ?)",
		"knobby", string(hash), "admin", true, true,
	)
	if err != nil {
		return fmt.Errorf("insert seed admin: %w", err)
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

// DeleteUser removes the user with the given id.
func (d *DB) DeleteUser(id int64) error {
	_, err := d.conn.Exec("DELETE FROM users WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	return nil
}
