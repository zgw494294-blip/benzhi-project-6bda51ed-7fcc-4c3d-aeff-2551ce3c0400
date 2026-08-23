package ledger

import "museum-label-governance/internal/label"

func PutDossier(st *State, d label.Dossier) error {
	if d.ID == "" {
		return ConstraintError{"id", "empty"}
	}
	if old, ok := st.Dossiers[d.ID]; ok && old.Status == label.StatusPublished && old.Version != d.Version {
		return ErrImmutable
	}
	st.Dossiers[d.ID] = d
	return nil
}
func AppendRevision(st *State, r label.Revision) error {
	if r.DossierID == "" || r.Number < 1 {
		return ConstraintError{"revision", "invalid"}
	}
	st.Revisions[r.DossierID] = append(st.Revisions[r.DossierID], r)
	return nil
}
