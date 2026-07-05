package db

import (
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

func Connect(databaseURL string) (*sqlx.DB, error) {
	dbx, err := sqlx.Connect("postgres", databaseURL)
	if err != nil {
		return nil, err
	}
	return dbx, nil
}
