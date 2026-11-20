package library

import (
	"context"
	"database/sql"
	"errors"
)

const (
	settingsLanguageKey     = "settings.language.v1"
	DefaultSettingsLanguage = "zh-CN"
)

var (
	ErrSettingsLanguage        = errors.New("default language must be zh-CN, zh-TW, or en")
	ErrSettingsLanguageCorrupt = errors.New("settings language is invalid")
)

func ValidateSettingsLanguage(language string) error {
	switch language {
	case "zh-CN", "zh-TW", "en":
		return nil
	default:
		return ErrSettingsLanguage
	}
}

func readSettingsLanguage(q interface{ QueryRow(string, ...any) *sql.Row }) (string, error) {
	var value string
	err := q.QueryRow(`SELECT value FROM server_state WHERE key=?`, settingsLanguageKey).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return DefaultSettingsLanguage, nil
	}
	if err != nil {
		return "", err
	}
	if err := ValidateSettingsLanguage(value); err != nil {
		return "", ErrSettingsLanguageCorrupt
	}
	return value, nil
}

func (s *Service) GetSettingsLanguage() (string, error) {
	return readSettingsLanguage(s.db)
}

func (s *Service) UpdateSettingsLanguage(language string) (string, error) {
	if err := ValidateSettingsLanguage(language); err != nil {
		return "", err
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	// Reserve the writer before reading, even when the preference is not yet stored.
	if _, err = tx.Exec(`UPDATE server_state SET value=value WHERE key=?`, settingsLanguageKey); err != nil {
		return "", err
	}
	if _, err = readSettingsLanguage(tx); err != nil {
		return "", err
	}
	if _, err = tx.Exec(`INSERT INTO server_state(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, settingsLanguageKey, language); err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return language, nil
}
