// Package apperr defines the closed set of API error codes and the error envelope
// { "error": { "code", "message", "details" } } (§4.4).
package apperr

import (
	"errors"
	"fmt"
	"net/http"
)

type Code string

const (
	InvalidRequest         Code = "invalid_request"
	IdempotencyKeyRequired Code = "idempotency_key_required"
	Unauthenticated        Code = "unauthenticated"
	CSRFFailed             Code = "csrf_failed"
	Forbidden              Code = "forbidden"
	NotFound               Code = "not_found"
	RevisionConflict       Code = "revision_conflict"
	RateLimited            Code = "rate_limited"
	EmailUnavailable       Code = "email_unavailable"
	InvalidCredentials     Code = "invalid_credentials"
	PasswordInvalid        Code = "password_invalid"
	PasswordTooCommon      Code = "password_too_common"
	TermsRequired          Code = "terms_required"
	TermsOutdated          Code = "terms_outdated"
	WebAppUnavailable      Code = "web_app_unavailable"
	CloudSyncUnavailable   Code = "cloud_sync_unavailable"
	ClientUpdateRequired   Code = "client_update_required"
	AccountSuspended       Code = "account_suspended"
	AccountDeleted         Code = "account_deleted"
	ReauthenticationNeeded Code = "reauthentication_required"
	ResetTokenInvalid      Code = "reset_token_invalid"
	InvalidField           Code = "invalid_field"
	LimitReached           Code = "limit_reached"
	SongTooLong            Code = "song_too_long"
	SongTooLarge           Code = "song_too_large"
	InvalidAudioMetadata   Code = "invalid_audio_metadata"
	UnsupportedFormat      Code = "unsupported_format"
	StorageQuotaExceeded   Code = "storage_quota_exceeded"
	OperationNotActive     Code = "operation_not_active"
	CursorExpired          Code = "cursor_expired"
	ArtistInUse            Code = "artist_in_use"
	Internal               Code = "internal"
)

var status = map[Code]int{
	InvalidRequest:         http.StatusBadRequest,
	IdempotencyKeyRequired: http.StatusBadRequest,
	Unauthenticated:        http.StatusUnauthorized,
	CSRFFailed:             http.StatusForbidden,
	Forbidden:              http.StatusForbidden,
	NotFound:               http.StatusNotFound,
	RevisionConflict:       http.StatusConflict,
	RateLimited:            http.StatusTooManyRequests,
	EmailUnavailable:       http.StatusConflict,
	InvalidCredentials:     http.StatusUnauthorized,
	PasswordInvalid:        http.StatusUnprocessableEntity,
	PasswordTooCommon:      http.StatusUnprocessableEntity,
	TermsRequired:          http.StatusUnprocessableEntity,
	TermsOutdated:          http.StatusForbidden,
	WebAppUnavailable:      http.StatusForbidden,
	CloudSyncUnavailable:   http.StatusForbidden,
	ClientUpdateRequired:   http.StatusUpgradeRequired,
	AccountSuspended:       http.StatusForbidden,
	AccountDeleted:         http.StatusGone,
	ReauthenticationNeeded: http.StatusForbidden,
	ResetTokenInvalid:      http.StatusGone,
	InvalidField:           http.StatusUnprocessableEntity,
	LimitReached:           http.StatusUnprocessableEntity,
	SongTooLong:            http.StatusUnprocessableEntity,
	SongTooLarge:           http.StatusUnprocessableEntity,
	InvalidAudioMetadata:   http.StatusUnprocessableEntity,
	UnsupportedFormat:      http.StatusUnprocessableEntity,
	StorageQuotaExceeded:   http.StatusUnprocessableEntity,
	OperationNotActive:     http.StatusConflict,
	CursorExpired:          http.StatusGone,
	ArtistInUse:            http.StatusConflict,
	Internal:               http.StatusInternalServerError,
}

// Error is an API error that maps to the envelope.
type Error struct {
	Code    Code           `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }

func (e *Error) Status() int {
	if s, ok := status[e.Code]; ok {
		return s
	}
	return http.StatusInternalServerError
}

func New(code Code, message string) *Error { return &Error{Code: code, Message: message} }

func WithDetails(code Code, message string, details map[string]any) *Error {
	return &Error{Code: code, Message: message, Details: details}
}

func Field(field, reason string) *Error {
	return WithDetails(InvalidField, "Invalid field: "+field, map[string]any{"field": field, "reason": reason})
}

// As extracts an *Error from err, or wraps unknown errors as internal.
func As(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

// Envelope is the JSON shape of every error response.
type Envelope struct {
	Error *Error `json:"error"`
}
