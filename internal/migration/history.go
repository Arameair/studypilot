package migration

import (
	"github.com/Arameair/studypilot/internal/schema"
	"time"
)

type HistoryRecord struct {
	SchemaVersion int                 `json:"schema_version"`
	MigrationID   string              `json:"migration_id"`
	DocumentType  schema.DocumentType `json:"document_type"`
	Path          string              `json:"path"`
	FromVersion   schema.Version      `json:"from_version"`
	ToVersion     schema.Version      `json:"to_version"`
	BeforeHash    string              `json:"before_hash"`
	AfterHash     string              `json:"after_hash"`
	AppliedAt     time.Time           `json:"applied_at"`
	Result        string              `json:"result"`
}
