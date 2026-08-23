package ledger

import (
	"encoding/json"
	"time"
)

type OperationRecord struct {
	Operation   string          `json:"operation"`
	DossierID   string          `json:"dossierId"`
	Fingerprint string          `json:"fingerprint"`
	Result      json.RawMessage `json:"result"`
	CreatedAt   time.Time       `json:"createdAt"`
}

func OperationKey(operation, dossierID, key string) string {
	return operation + "|" + dossierID + "|" + key
}

func GetOperation(st State, operation, dossierID, key string) (OperationRecord, bool) {
	if key == "" {
		return OperationRecord{}, false
	}
	r, ok := st.Operations[OperationKey(operation, dossierID, key)]
	return r, ok
}

func PutOperation(st *State, operation, dossierID, key, fingerprint string, result any) error {
	if key == "" {
		return nil
	}
	b, err := json.Marshal(result)
	if err != nil {
		return err
	}
	st.Operations[OperationKey(operation, dossierID, key)] = OperationRecord{Operation: operation, DossierID: dossierID, Fingerprint: fingerprint, Result: b, CreatedAt: time.Now()}
	return nil
}

func (s *Store) Idempotent(key string, result any) (bool, error) {
	if key == "" {
		return false, nil
	}
	b, _ := json.Marshal(result)
	var existed bool
	e := s.Update(func(st *State) error {
		if _, ok := st.Idempotency[key]; ok {
			existed = true
			return nil
		}
		st.Idempotency[key] = b
		return nil
	})
	return existed, e
}
func (s *Store) IdempotentResult(key string) (json.RawMessage, bool) {
	var b json.RawMessage
	s.View(func(st State) error { b = st.Idempotency[key]; return nil })
	return b, b != nil
}
