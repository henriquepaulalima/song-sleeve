// Package jobs declares the River job arguments (D-2). Handlers insert jobs in the
// same transaction as the change that needs them; the worker package runs them.
package jobs

import (
	"time"

	"github.com/google/uuid"
)

// SendEmail delivers one transactional email. Link material is sealed at rest.
type SendEmail struct {
	UserID uuid.UUID `json:"user_id"`
	Email  string    `json:"email_kind"` // "password_reset" | "password_changed"
	// SealedLink holds the reset URL encrypted with DATA_KEY; empty for notices.
	SealedLink string    `json:"sealed_link,omitempty"`
	ExpiresAt  time.Time `json:"expires_at"`
}

func (SendEmail) Kind() string { return "send_email" }

// VerifyPublish verifies an uploaded staging object and publishes it (R-CLOUD-5).
type VerifyPublish struct {
	UserID      uuid.UUID `json:"user_id"`
	OperationID uuid.UUID `json:"operation_id"`
}

func (VerifyPublish) Kind() string { return "verify_publish" }

// CleanupObjects deletes bucket objects and aborts multipart uploads. Retried until
// it succeeds; scheduled after any download grants for the objects expire.
type CleanupObjects struct {
	Keys    []string         `json:"keys,omitempty"`
	Uploads []MultipartAbort `json:"uploads,omitempty"`
}

type MultipartAbort struct {
	Key      string `json:"key"`
	UploadID string `json:"upload_id"`
}

func (CleanupObjects) Kind() string { return "cleanup_objects" }

// AccountDeletion drives an accepted, irreversible account deletion (R-AUTH-5).
type AccountDeletion struct {
	RequestID uuid.UUID `json:"request_id"`
	UserID    uuid.UUID `json:"user_id"`
}

func (AccountDeletion) Kind() string { return "account_deletion" }

// Periodic maintenance.
type StallSweep struct{}

func (StallSweep) Kind() string { return "stall_sweep" }

type PromoteOperations struct{}

func (PromoteOperations) Kind() string { return "promote_operations" }

type Retention struct{}

func (Retention) Kind() string { return "retention" }
