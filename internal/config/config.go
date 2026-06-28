package config

import (
	"errors"
	"os"
	"path/filepath"
)

const (
	DefaultEmbeddingModel = "mxbai-embed-large"
	DefaultHomeName       = ".memo"
	DefaultDBName         = "memo.db"
)

type Paths struct {
	Home string
	DB   string
}

func Resolve(homeOverride, dbOverride string) (Paths, error) {
	home := homeOverride
	if home == "" {
		home = os.Getenv("MEMO_HOME")
	}
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, err
		}
		home = filepath.Join(userHome, DefaultHomeName)
	}

	db := dbOverride
	if db == "" {
		db = os.Getenv("MEMO_DB_PATH")
	}
	if db == "" {
		db = filepath.Join(home, DefaultDBName)
	}
	if home == "" || db == "" {
		return Paths{}, errors.New("memo paths cannot be empty")
	}
	return Paths{Home: filepath.Clean(home), DB: filepath.Clean(db)}, nil
}
