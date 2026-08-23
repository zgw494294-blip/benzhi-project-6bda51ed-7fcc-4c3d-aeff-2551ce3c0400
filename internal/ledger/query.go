package ledger

import (
	"museum-label-governance/internal/label"
	"sort"
	"time"
)

type DossierFilter struct {
	Status         label.Status
	ExhibitionName string
	Owner          string
	AfterCreatedAt time.Time
	AfterID        string
	Limit          int
}

func (s *Store) QueryDossiers(filter DossierFilter) ([]label.Dossier, bool) {
	limit := filter.Limit
	if limit <= 0 || limit > label.MaxListItems {
		limit = label.MaxListItems
	}
	var all []label.Dossier
	_ = s.View(func(st State) error {
		for _, d := range st.Dossiers {
			if filter.Status != "" && d.Status != filter.Status {
				continue
			}
			if filter.ExhibitionName != "" && d.ExhibitionName != filter.ExhibitionName {
				continue
			}
			if filter.Owner != "" && d.Owner != filter.Owner {
				continue
			}
			if !filter.AfterCreatedAt.IsZero() && (d.CreatedAt.Before(filter.AfterCreatedAt) || (d.CreatedAt.Equal(filter.AfterCreatedAt) && d.ID <= filter.AfterID)) {
				continue
			}
			all = append(all, d)
		}
		return nil
	})
	sort.Slice(all, func(i, j int) bool {
		if all[i].CreatedAt.Equal(all[j].CreatedAt) {
			return all[i].ID < all[j].ID
		}
		return all[i].CreatedAt.Before(all[j].CreatedAt)
	})
	hasMore := len(all) > limit
	if hasMore {
		all = all[:limit]
	}
	if all == nil {
		all = []label.Dossier{}
	}
	return all, hasMore
}

func (s *Store) ListDossiers(limit int) []label.Dossier {
	out, _ := s.QueryDossiers(DossierFilter{Limit: limit})
	return out
}
func (s *Store) Snapshot(id string) (label.Snapshot, error) {
	var v label.Snapshot
	e := s.View(func(st State) error {
		var ok bool
		v, ok = st.Snapshots[id]
		if !ok {
			return ErrNotFound
		}
		return nil
	})
	return v, e
}
func (s *Store) Credential(no string) (label.Credential, error) {
	var v label.Credential
	e := s.View(func(st State) error {
		var ok bool
		v, ok = st.Credentials[no]
		if !ok {
			return ErrNotFound
		}
		return nil
	})
	return v, e
}
