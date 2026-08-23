package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"museum-label-governance/internal/label"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var ErrNotFound = errors.New("not found")
var ErrConflict = errors.New("version conflict")
var ErrImmutable = errors.New("immutable record")

type State struct {
	SchemaVersion int                                  `json:"schemaVersion"`
	Dossiers      map[string]label.Dossier             `json:"dossiers"`
	Revisions     map[string][]label.Revision          `json:"revisions"`
	Claims        map[string][]label.Claim             `json:"claims"`
	Evidence      map[string][]label.Evidence          `json:"evidence"`
	Snapshots     map[string]label.Snapshot            `json:"snapshots"`
	Credentials   map[string]label.Credential          `json:"credentials"`
	Audits        map[string][]label.AuditEvent        `json:"audits"`
	Idempotency   map[string]json.RawMessage           `json:"idempotency"`
	Operations    map[string]OperationRecord           `json:"operations"`
	Suggestions   map[string][]label.CopySuggestion    `json:"suggestions"`
	Prechecks     map[string][]label.PrecheckSnapshot  `json:"prechecks"`
	ExpertDrafts  map[string][]label.ExpertReviewDraft `json:"expertDrafts"`
	QueryAudits   []label.AuditEvent                   `json:"queryAudits"`
}

type Store struct {
	mu   sync.RWMutex
	path string
	s    State
}

func New(path string) (*Store, error) {
	st := &Store{path: path}
	st.s = newState()
	if b, e := os.ReadFile(path); e == nil {
		if e = json.Unmarshal(b, &st.s); e != nil {
			return nil, e
		}
		st.ensure()
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	return st, nil
}

func newState() State {
	return State{SchemaVersion: CurrentSchemaVersion, Dossiers: map[string]label.Dossier{}, Revisions: map[string][]label.Revision{}, Claims: map[string][]label.Claim{}, Evidence: map[string][]label.Evidence{}, Snapshots: map[string]label.Snapshot{}, Credentials: map[string]label.Credential{}, Audits: map[string][]label.AuditEvent{}, Idempotency: map[string]json.RawMessage{}, Operations: map[string]OperationRecord{}, Suggestions: map[string][]label.CopySuggestion{}, Prechecks: map[string][]label.PrecheckSnapshot{}, ExpertDrafts: map[string][]label.ExpertReviewDraft{}}
}
func (s *Store) ensure() {
	if s.s.Dossiers == nil {
		s.s.Dossiers = map[string]label.Dossier{}
	}
	if s.s.Revisions == nil {
		s.s.Revisions = map[string][]label.Revision{}
	}
	if s.s.Claims == nil {
		s.s.Claims = map[string][]label.Claim{}
	}
	if s.s.Evidence == nil {
		s.s.Evidence = map[string][]label.Evidence{}
	}
	if s.s.Snapshots == nil {
		s.s.Snapshots = map[string]label.Snapshot{}
	}
	if s.s.Credentials == nil {
		s.s.Credentials = map[string]label.Credential{}
	}
	if s.s.Audits == nil {
		s.s.Audits = map[string][]label.AuditEvent{}
	}
	if s.s.Idempotency == nil {
		s.s.Idempotency = map[string]json.RawMessage{}
	}
	if s.s.Operations == nil {
		s.s.Operations = map[string]OperationRecord{}
	}
	if s.s.Suggestions == nil {
		s.s.Suggestions = map[string][]label.CopySuggestion{}
	}
	if s.s.Prechecks == nil {
		s.s.Prechecks = map[string][]label.PrecheckSnapshot{}
	}
	if s.s.ExpertDrafts == nil {
		s.s.ExpertDrafts = map[string][]label.ExpertReviewDraft{}
	}
}
func (s *Store) persistLocked(st State) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	b, e := json.MarshalIndent(st, "", "  ")
	if e != nil {
		return e
	}
	tmp := s.path + ".tmp"
	if e = os.WriteFile(tmp, b, 0644); e != nil {
		return e
	}
	return os.Rename(tmp, s.path)
}
func (s *Store) Update(fn func(*State) error) error {
	return s.UpdateContext(context.Background(), fn)
}

func (s *Store) UpdateContext(ctx context.Context, fn func(*State) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, e := json.Marshal(s.s)
	if e != nil {
		return e
	}
	next := newState()
	if e = json.Unmarshal(b, &next); e != nil {
		return e
	}
	if e = fn(&next); e != nil {
		return e
	}
	if e = s.persistLocked(next); e != nil {
		return e
	}
	s.s = next
	return nil
}
func (s *Store) View(fn func(State) error) error { s.mu.RLock(); defer s.mu.RUnlock(); return fn(s.s) }
func (s *Store) GetDossier(id string) (label.Dossier, error) {
	var d label.Dossier
	e := s.View(func(st State) error {
		var ok bool
		d, ok = st.Dossiers[id]
		if !ok {
			return ErrNotFound
		}
		return nil
	})
	return d, e
}
func (s *Store) Revisions(id string) []label.Revision {
	var x []label.Revision
	s.View(func(st State) error { x = append(x, st.Revisions[id]...); return nil })
	return x
}
func (s *Store) Claims(id string, rev int) []label.Claim {
	var x []label.Claim
	s.View(func(st State) error {
		for _, c := range st.Claims[id] {
			if c.RevisionNo == rev {
				x = append(x, c)
			}
		}
		return nil
	})
	return x
}
func (s *Store) Evidence(id string) []label.Evidence {
	var x []label.Evidence
	s.View(func(st State) error { x = append(x, st.Evidence[id]...); return nil })
	return x
}
func (s *Store) Audits(id string) []label.AuditEvent {
	var x []label.AuditEvent
	s.View(func(st State) error { x = append(x, st.Audits[id]...); return nil })
	return x
}
func AddAudit(st *State, id, action, actor, detail string) {
	st.Audits[id] = append(st.Audits[id], label.AuditEvent{ID: time.Now().Format("20060102150405.000000000"), DossierID: id, Action: action, Actor: actor, Detail: detail, At: time.Now()})
}
