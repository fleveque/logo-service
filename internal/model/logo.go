// Package model defines the core data types for the logo service.
// In Go, we use structs instead of classes. Struct tags (the `json:"..."` and
// `db:"..."` annotations) tell serialization libraries how to map fields.
package model

import "time"

// LogoSize represents the available image sizes.
// Go doesn't have enums — we use typed constants with iota or explicit values.
type LogoSize string

const (
	SizeXS LogoSize = "xs" // 16px
	SizeS  LogoSize = "s"  // 32px
	SizeM  LogoSize = "m"  // 64px
	SizeL  LogoSize = "l"  // 128px
	SizeXL LogoSize = "xl" // 256px
)

// SizePixels maps each LogoSize to its pixel dimension.
var SizePixels = map[LogoSize]int{
	SizeXS: 16,
	SizeS:  32,
	SizeM:  64,
	SizeL:  128,
	SizeXL: 256,
}

// AllSizes is the ordered list of all sizes for iteration.
var AllSizes = []LogoSize{SizeXS, SizeS, SizeM, SizeL, SizeXL}

// ValidSize checks if a string is a valid LogoSize.
func ValidSize(s string) bool {
	_, ok := SizePixels[LogoSize(s)]
	return ok
}

// LogoStatus represents the processing state of a logo.
type LogoStatus string

const (
	StatusPending    LogoStatus = "pending"
	StatusProcessed  LogoStatus = "processed"
	StatusFailed     LogoStatus = "failed"
	StatusNotFound   LogoStatus = "not_found"
)

// Logo is the main domain entity. Each field has two tags:
//   - `db:"column_name"` — used by sqlx to scan database rows
//   - `json:"field_name"` — used for JSON serialization (API responses)
type Logo struct {
	ID           int64      `db:"id" json:"id"`
	Symbol       string     `db:"symbol" json:"symbol"`
	CompanyName  string     `db:"company_name" json:"company_name"`
	Source       string     `db:"source" json:"source"`
	OriginalURL  string     `db:"original_url" json:"original_url"`
	HasXS        bool       `db:"has_xs" json:"has_xs"`
	HasS         bool       `db:"has_s" json:"has_s"`
	HasM         bool       `db:"has_m" json:"has_m"`
	HasL         bool       `db:"has_l" json:"has_l"`
	HasXL        bool       `db:"has_xl" json:"has_xl"`
	Status       LogoStatus `db:"status" json:"status"`
	ErrorMessage *string    `db:"error_message" json:"error_message,omitempty"`
	CreatedAt    time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time  `db:"updated_at" json:"updated_at"`
}

// HasSize returns whether the logo has been processed at the given size.
// This uses a switch statement — Go's switch doesn't need `break` (it's implicit).
func (l *Logo) HasSize(size LogoSize) bool {
	switch size {
	case SizeXS:
		return l.HasXS
	case SizeS:
		return l.HasS
	case SizeM:
		return l.HasM
	case SizeL:
		return l.HasL
	case SizeXL:
		return l.HasXL
	default:
		return false
	}
}

// LLMCall tracks each call to an LLM provider for cost monitoring.
type LLMCall struct {
	ID         int64     `db:"id" json:"id"`
	Symbol     string    `db:"symbol" json:"symbol"`
	Provider   string    `db:"provider" json:"provider"`
	Model      string    `db:"model" json:"model"`
	ResultURL  *string   `db:"result_url" json:"result_url,omitempty"`
	Success    bool      `db:"success" json:"success"`
	DurationMs *int64    `db:"duration_ms" json:"duration_ms,omitempty"`
	CreatedAt  time.Time `db:"created_at" json:"created_at"`
}
