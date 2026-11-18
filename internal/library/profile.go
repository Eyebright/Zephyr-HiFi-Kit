package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"
)

const settingsProfileKey = "settings.profile.v1"

var (
	ErrSettingsProfileCorrupt = errors.New("settings profile is invalid")
	ErrSettingsDisplayName    = errors.New("display name must be 1..40 Unicode characters without control characters")
	ErrSettingsOnboarding     = errors.New("onboarding requires a display name, step 1..4, and completion at step 4")
)

// Credentials and session tokens must never be stored in this public profile.
type SettingsProfile struct {
	DisplayName        string `json:"display_name"`
	OnboardingStep     int    `json:"onboarding_step"`
	OnboardingComplete bool   `json:"onboarding_complete"`
	Legacy             bool   `json:"legacy"`
}

func InitialSettingsProfile(configured bool) SettingsProfile {
	if configured {
		return SettingsProfile{OnboardingStep: 4, OnboardingComplete: true, Legacy: true}
	}
	return SettingsProfile{OnboardingStep: 1}
}

// Names are plain text, never trusted HTML.
func NormalizeSettingsDisplayName(name string) (string, error) {
	if !utf8.ValidString(name) {
		return "", ErrSettingsDisplayName
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", ErrSettingsDisplayName
		}
	}
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n < 1 || n > 40 {
		return "", ErrSettingsDisplayName
	}
	return name, nil
}

func (p SettingsProfile) WithDisplayName(name string) (SettingsProfile, error) {
	name, err := NormalizeSettingsDisplayName(name)
	if err != nil {
		return p, err
	}
	p.DisplayName = name
	return p, nil
}

// WithOnboarding deliberately allows revisiting earlier steps. Authentication
// and the existence of credentials are checked by the API, not inferred here.
func (p SettingsProfile) WithOnboarding(step int, complete bool) (SettingsProfile, error) {
	if _, err := NormalizeSettingsDisplayName(p.DisplayName); err != nil || step < 1 || step > 4 || (complete && step != 4) {
		return p, ErrSettingsOnboarding
	}
	p.OnboardingStep, p.OnboardingComplete = step, complete
	return p, nil
}

func decodeSettingsProfile(value string) (SettingsProfile, error) {
	// Pointers distinguish missing/null fields from legitimate false/empty values.
	var wire struct {
		DisplayName        *string `json:"display_name"`
		OnboardingStep     *int    `json:"onboarding_step"`
		OnboardingComplete *bool   `json:"onboarding_complete"`
		Legacy             *bool   `json:"legacy"`
	}
	if !utf8.ValidString(value) || len(value) > 4096 {
		return SettingsProfile{}, ErrSettingsProfileCorrupt
	}
	d := json.NewDecoder(strings.NewReader(value))
	d.DisallowUnknownFields()
	if err := d.Decode(&wire); err != nil || wire.DisplayName == nil || wire.OnboardingStep == nil || wire.OnboardingComplete == nil || wire.Legacy == nil {
		return SettingsProfile{}, ErrSettingsProfileCorrupt
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return SettingsProfile{}, ErrSettingsProfileCorrupt
	}
	p := SettingsProfile{*wire.DisplayName, *wire.OnboardingStep, *wire.OnboardingComplete, *wire.Legacy}
	if p.OnboardingStep < 1 || p.OnboardingStep > 4 || (p.OnboardingComplete && p.OnboardingStep != 4) {
		return SettingsProfile{}, ErrSettingsProfileCorrupt
	}
	if p.DisplayName == "" {
		if !p.Legacy && p.OnboardingStep != 1 {
			return SettingsProfile{}, ErrSettingsProfileCorrupt
		}
	} else if name, err := NormalizeSettingsDisplayName(p.DisplayName); err != nil || name != p.DisplayName {
		return SettingsProfile{}, ErrSettingsProfileCorrupt
	}
	return p, nil
}

func readSettingsProfile(q interface{ QueryRow(string, ...any) *sql.Row }) (SettingsProfile, error) {
	var value string
	if err := q.QueryRow(`SELECT value FROM server_state WHERE key=?`, settingsProfileKey).Scan(&value); err != nil {
		return SettingsProfile{}, err
	}
	return decodeSettingsProfile(value)
}

// Preserve the pre-credential step-1 marker across restarts and reject malformed state.
func (s *Service) InitializeSettingsProfile(configured bool) (SettingsProfile, error) {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return SettingsProfile{}, err
	}
	defer tx.Rollback()
	value, err := json.Marshal(InitialSettingsProfile(configured))
	if err != nil {
		return SettingsProfile{}, err
	}
	if _, err = tx.Exec(`INSERT INTO server_state(key,value) VALUES(?,?) ON CONFLICT(key) DO NOTHING`, settingsProfileKey, string(value)); err != nil {
		return SettingsProfile{}, err
	}
	p, err := readSettingsProfile(tx)
	if err != nil {
		return SettingsProfile{}, err
	}
	return p, tx.Commit()
}

func (s *Service) GetSettingsProfile() (SettingsProfile, error) {
	return readSettingsProfile(s.db)
}

// updateSettingsProfile acquires SQLite's writer reservation before reading, so
// independent service instances/connections cannot overwrite one another's fields.
func (s *Service) updateSettingsProfile(update func(SettingsProfile) (SettingsProfile, error)) (SettingsProfile, error) {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return SettingsProfile{}, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`UPDATE server_state SET value=value WHERE key=?`, settingsProfileKey); err != nil {
		return SettingsProfile{}, err
	}
	p, err := readSettingsProfile(tx)
	if err != nil {
		return SettingsProfile{}, err
	}
	p, err = update(p)
	if err != nil {
		return SettingsProfile{}, err
	}
	value, err := json.Marshal(p)
	if err != nil {
		return SettingsProfile{}, err
	}
	if _, err = tx.Exec(`UPDATE server_state SET value=? WHERE key=?`, string(value), settingsProfileKey); err != nil {
		return SettingsProfile{}, err
	}
	return p, tx.Commit()
}

func (s *Service) UpdateSettingsDisplayName(name string) (SettingsProfile, error) {
	return s.updateSettingsProfile(func(p SettingsProfile) (SettingsProfile, error) {
		return p.WithDisplayName(name)
	})
}

func (s *Service) UpdateSettingsOnboarding(step int, complete bool) (SettingsProfile, error) {
	return s.updateSettingsProfile(func(p SettingsProfile) (SettingsProfile, error) {
		return p.WithOnboarding(step, complete)
	})
}

// CompleteSettingsSetup is called only after exclusive credential creation. Its
// failure leaves the step-1 marker intact, allowing login and profile repair.
func (s *Service) CompleteSettingsSetup(name string) (SettingsProfile, error) {
	return s.updateSettingsProfile(func(p SettingsProfile) (SettingsProfile, error) {
		p, err := p.WithDisplayName(name)
		if err != nil {
			return p, err
		}
		p.OnboardingStep, p.OnboardingComplete, p.Legacy = 2, false, false
		return p, nil
	})
}
