package db

import (
	"github.com/jmoiron/sqlx"
	"golang.org/x/crypto/bcrypt"
)

// SeedAdmin creates the v1 single admin account from ADMIN_USERNAME/
// ADMIN_PASSWORD if the admins table is empty. There's no admin-management
// UI/API in v1 (see docs/PLAN.md), so this env-var seed is the only way in.
func SeedAdmin(dbx *sqlx.DB, username, password string) error {
	if username == "" || password == "" {
		return nil
	}

	var count int
	if err := dbx.Get(&count, `SELECT count(*) FROM admins`); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	_, err = dbx.Exec(`INSERT INTO admins (username, password_hash) VALUES ($1, $2)`, username, hash)
	return err
}
