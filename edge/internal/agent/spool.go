package agent

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	spoolVersion        = 1
	spoolExtension      = ".msg"
	maxSpoolHeaderBytes = 4096
	maxSpoolIDLength    = 128
)

var (
	ErrInvalidSpoolConfig = errors.New("store-and-forward configuration is invalid")
	ErrInvalidSpoolItem   = errors.New("store-and-forward item is invalid")
	ErrSpoolConflict      = errors.New("store-and-forward item conflicts with an existing id")
	ErrCorruptSpool       = errors.New("store-and-forward data is corrupt")
	ErrSpoolEmpty         = errors.New("store-and-forward queue is empty")
	ErrSpoolItemNotFound  = errors.New("store-and-forward item was not found")
)

type SpoolPriority string

const (
	SpoolMetadata SpoolPriority = "metadata"
	SpoolEvidence SpoolPriority = "evidence"
	SpoolVideo    SpoolPriority = "video"
)

type SpoolItem struct {
	ID        string
	Priority  SpoolPriority
	CreatedAt time.Time
	SizeBytes int64
}

type SpoolMessage struct {
	SpoolItem
	Payload []byte
}

type SpoolMetrics struct {
	Depth         int64
	Bytes         int64
	EnqueuedTotal uint64
	AckedTotal    uint64
	EvictedTotal  uint64
	OldestAge     time.Duration
}

type spoolEnvelope struct {
	Version       int           `json:"version"`
	ID            string        `json:"id"`
	Priority      SpoolPriority `json:"priority"`
	CreatedAt     time.Time     `json:"created_at"`
	PayloadBytes  int64         `json:"payload_bytes"`
	PayloadSHA256 string        `json:"payload_sha256"`
}

type spoolEntry struct {
	item        SpoolItem
	path        string
	fileBytes   int64
	payloadHash string
}

// DurableSpool is a restart-safe local queue for opaque event and evidence
// payloads. Files are written atomically with restrictive permissions. Quota
// enforcement evicts the globally oldest item first; replay remains priority
// ordered as metadata, evidence, then archive video.
type DurableSpool struct {
	mu           sync.Mutex
	root         string
	maxBytes     int64
	maxItemBytes int64
	now          func() time.Time
	entries      map[string]spoolEntry
	metrics      SpoolMetrics
}

func NewDurableSpool(root string, maxBytes, maxItemBytes int64) (*DurableSpool, error) {
	root = strings.TrimSpace(root)
	if root == "" || maxBytes <= 0 || maxItemBytes <= 0 || maxItemBytes > maxBytes {
		return nil, ErrInvalidSpoolConfig
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, ErrInvalidSpoolConfig
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, fmt.Errorf("create store-and-forward directory: %w", err)
	}
	if err := os.Chmod(abs, 0o700); err != nil {
		return nil, fmt.Errorf("restrict store-and-forward directory: %w", err)
	}
	spool := &DurableSpool{
		root:         abs,
		maxBytes:     maxBytes,
		maxItemBytes: maxItemBytes,
		now:          func() time.Time { return time.Now().UTC() },
		entries:      make(map[string]spoolEntry),
	}
	if err := spool.recover(); err != nil {
		return nil, err
	}
	if err := spool.enforceQuota(""); err != nil {
		return nil, err
	}
	return spool, nil
}

// Enqueue durably stores one opaque payload. Repeating the same ID, priority,
// and payload is idempotent; reusing an ID for different data fails closed.
func (s *DurableSpool) Enqueue(id string, priority SpoolPriority, payload []byte) (SpoolItem, error) {
	if s == nil {
		return SpoolItem{}, ErrInvalidSpoolConfig
	}
	id = strings.TrimSpace(id)
	if !validSpoolID(id) || !validSpoolPriority(priority) || len(payload) == 0 || int64(len(payload)) > s.maxItemBytes {
		return SpoolItem{}, ErrInvalidSpoolItem
	}
	payloadHash := hashPayload(payload)

	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.entries[id]; ok {
		if existing.item.Priority == priority && existing.payloadHash == payloadHash {
			return existing.item, nil
		}
		return SpoolItem{}, ErrSpoolConflict
	}

	createdAt := s.now().UTC()
	envelope := spoolEnvelope{
		Version:       spoolVersion,
		ID:            id,
		Priority:      priority,
		CreatedAt:     createdAt,
		PayloadBytes:  int64(len(payload)),
		PayloadSHA256: payloadHash,
	}
	record, err := encodeSpoolRecord(envelope, payload)
	if err != nil || int64(len(record)) > s.maxBytes {
		return SpoolItem{}, ErrInvalidSpoolItem
	}
	path := filepath.Join(s.root, spoolFilename(createdAt, id))
	if err := writeSpoolFileAtomic(s.root, path, record); err != nil {
		return SpoolItem{}, err
	}
	entry := spoolEntry{
		item:        SpoolItem{ID: id, Priority: priority, CreatedAt: createdAt, SizeBytes: int64(len(payload))},
		path:        path,
		fileBytes:   int64(len(record)),
		payloadHash: payloadHash,
	}
	s.entries[id] = entry
	s.metrics.Depth++
	s.metrics.Bytes += entry.fileBytes
	s.metrics.EnqueuedTotal++
	if err := s.enforceQuota(id); err != nil {
		return SpoolItem{}, err
	}
	return entry.item, nil
}

// Next returns a verified copy of the next replay item without removing it.
// Metadata is replayed before evidence, then archive video; order is FIFO
// within a priority class.
func (s *DurableSpool) Next() (SpoolMessage, error) {
	if s == nil {
		return SpoolMessage{}, ErrInvalidSpoolConfig
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries := s.sortedEntries(false)
	if len(entries) == 0 {
		return SpoolMessage{}, ErrSpoolEmpty
	}
	envelope, payload, _, err := readSpoolFile(entries[0].path, s.maxItemBytes)
	if err != nil || envelope.ID != entries[0].item.ID || envelope.PayloadSHA256 != entries[0].payloadHash {
		return SpoolMessage{}, ErrCorruptSpool
	}
	return SpoolMessage{SpoolItem: entries[0].item, Payload: payload}, nil
}

// Ack removes an item only after its upstream consumer has acknowledged it.
func (s *DurableSpool) Ack(id string) error {
	id = strings.TrimSpace(id)
	if s == nil || !validSpoolID(id) {
		return ErrInvalidSpoolItem
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[id]
	if !ok {
		return ErrSpoolItemNotFound
	}
	if err := os.Remove(entry.path); err != nil {
		return fmt.Errorf("acknowledge store-and-forward item: %w", err)
	}
	delete(s.entries, id)
	s.metrics.Depth--
	s.metrics.Bytes -= entry.fileBytes
	s.metrics.AckedTotal++
	return nil
}

func (s *DurableSpool) Metrics() SpoolMetrics {
	if s == nil {
		return SpoolMetrics{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	metrics := s.metrics
	oldest := s.sortedEntries(true)
	if len(oldest) > 0 {
		metrics.OldestAge = s.now().UTC().Sub(oldest[0].item.CreatedAt)
		if metrics.OldestAge < 0 {
			metrics.OldestAge = 0
		}
	}
	return metrics
}

func (s *DurableSpool) recover() error {
	directoryEntries, err := os.ReadDir(s.root)
	if err != nil {
		return fmt.Errorf("read store-and-forward directory: %w", err)
	}
	for _, directoryEntry := range directoryEntries {
		if strings.HasPrefix(directoryEntry.Name(), ".spool-") {
			if directoryEntry.IsDir() {
				return ErrCorruptSpool
			}
			if err := os.Remove(filepath.Join(s.root, directoryEntry.Name())); err != nil {
				return fmt.Errorf("remove incomplete store-and-forward item: %w", err)
			}
			continue
		}
		if directoryEntry.IsDir() || !strings.HasSuffix(directoryEntry.Name(), spoolExtension) {
			continue
		}
		info, err := directoryEntry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return ErrCorruptSpool
		}
		path := filepath.Join(s.root, directoryEntry.Name())
		envelope, _, fileBytes, err := readSpoolFile(path, s.maxItemBytes)
		if err != nil || !validSpoolID(envelope.ID) || !validSpoolPriority(envelope.Priority) {
			return ErrCorruptSpool
		}
		if _, duplicate := s.entries[envelope.ID]; duplicate {
			return ErrCorruptSpool
		}
		entry := spoolEntry{
			item:        SpoolItem{ID: envelope.ID, Priority: envelope.Priority, CreatedAt: envelope.CreatedAt.UTC(), SizeBytes: envelope.PayloadBytes},
			path:        path,
			fileBytes:   fileBytes,
			payloadHash: envelope.PayloadSHA256,
		}
		s.entries[envelope.ID] = entry
		s.metrics.Depth++
		s.metrics.Bytes += fileBytes
	}
	return nil
}

func (s *DurableSpool) enforceQuota(protectedID string) error {
	for s.metrics.Bytes > s.maxBytes {
		entries := s.sortedEntries(true)
		victimIndex := 0
		for victimIndex < len(entries) && entries[victimIndex].item.ID == protectedID {
			victimIndex++
		}
		if victimIndex == len(entries) {
			return ErrInvalidSpoolItem
		}
		victim := entries[victimIndex]
		if err := os.Remove(victim.path); err != nil {
			return fmt.Errorf("evict store-and-forward item: %w", err)
		}
		delete(s.entries, victim.item.ID)
		s.metrics.Depth--
		s.metrics.Bytes -= victim.fileBytes
		s.metrics.EvictedTotal++
	}
	return nil
}

func (s *DurableSpool) sortedEntries(oldestOnly bool) []spoolEntry {
	entries := make([]spoolEntry, 0, len(s.entries))
	for _, entry := range s.entries {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(left, right int) bool {
		if !oldestOnly {
			leftPriority := spoolPriorityRank(entries[left].item.Priority)
			rightPriority := spoolPriorityRank(entries[right].item.Priority)
			if leftPriority != rightPriority {
				return leftPriority < rightPriority
			}
		}
		if entries[left].item.CreatedAt.Equal(entries[right].item.CreatedAt) {
			return entries[left].item.ID < entries[right].item.ID
		}
		return entries[left].item.CreatedAt.Before(entries[right].item.CreatedAt)
	})
	return entries
}

func encodeSpoolRecord(envelope spoolEnvelope, payload []byte) ([]byte, error) {
	header, err := json.Marshal(envelope)
	if err != nil || len(header) > maxSpoolHeaderBytes {
		return nil, ErrInvalidSpoolItem
	}
	record := make([]byte, 0, len(header)+1+len(payload))
	record = append(record, header...)
	record = append(record, '\n')
	record = append(record, payload...)
	return record, nil
}

func writeSpoolFileAtomic(root, destination string, record []byte) error {
	temporary, err := os.CreateTemp(root, ".spool-*")
	if err != nil {
		return fmt.Errorf("create store-and-forward item: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("restrict store-and-forward item: %w", err)
	}
	if _, err := temporary.Write(record); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write store-and-forward item: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync store-and-forward item: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close store-and-forward item: %w", err)
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return fmt.Errorf("commit store-and-forward item: %w", err)
	}
	return nil
}

func readSpoolFile(path string, maxItemBytes int64) (spoolEnvelope, []byte, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return spoolEnvelope{}, nil, 0, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() <= 0 || info.Size() > maxItemBytes+maxSpoolHeaderBytes+1 {
		return spoolEnvelope{}, nil, 0, ErrCorruptSpool
	}
	reader := bufio.NewReader(io.LimitReader(file, info.Size()))
	header, err := reader.ReadBytes('\n')
	if err != nil || len(header) <= 1 || len(header) > maxSpoolHeaderBytes+1 {
		return spoolEnvelope{}, nil, 0, ErrCorruptSpool
	}
	var envelope spoolEnvelope
	if err := json.Unmarshal(bytes.TrimSuffix(header, []byte{'\n'}), &envelope); err != nil || envelope.Version != spoolVersion || envelope.CreatedAt.IsZero() || envelope.PayloadBytes <= 0 || envelope.PayloadBytes > maxItemBytes {
		return spoolEnvelope{}, nil, 0, ErrCorruptSpool
	}
	payload, err := io.ReadAll(io.LimitReader(reader, maxItemBytes+1))
	if err != nil || int64(len(payload)) != envelope.PayloadBytes || hashPayload(payload) != envelope.PayloadSHA256 {
		return spoolEnvelope{}, nil, 0, ErrCorruptSpool
	}
	return envelope, payload, info.Size(), nil
}

func spoolFilename(createdAt time.Time, id string) string {
	return fmt.Sprintf("%020d-%s%s", createdAt.UnixNano(), id, spoolExtension)
}

func validSpoolID(id string) bool {
	if id == "" || len(id) > maxSpoolIDLength {
		return false
	}
	for _, character := range id {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}

func validSpoolPriority(priority SpoolPriority) bool {
	return priority == SpoolMetadata || priority == SpoolEvidence || priority == SpoolVideo
}

func spoolPriorityRank(priority SpoolPriority) int {
	switch priority {
	case SpoolMetadata:
		return 0
	case SpoolEvidence:
		return 1
	default:
		return 2
	}
}

func hashPayload(payload []byte) string {
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}
