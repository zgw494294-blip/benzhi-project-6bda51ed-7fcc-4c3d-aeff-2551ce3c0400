package ledger

const CurrentSchemaVersion = 3

func (s *Store) SchemaVersion() int {
	var v int
	_ = s.View(func(st State) error { v = st.SchemaVersion; return nil })
	return v
}
func (s *Store) Migrate() error {
	return s.Update(func(st *State) error {
		if st.SchemaVersion < CurrentSchemaVersion {
			for dossierID, items := range st.Evidence {
				for i := range items {
					if items[i].Status == "" {
						items[i].Status = "active"
					}
				}
				st.Evidence[dossierID] = items
			}
			for dossierID, items := range st.Suggestions {
				for i := range items {
					if items[i].Status == "" {
						if items[i].Resolved {
							items[i].Status = "applied"
						} else {
							items[i].Status = "pending"
						}
					}
				}
				st.Suggestions[dossierID] = items
			}
			for no, credential := range st.Credentials {
				if credential.RevisionNo == 0 {
					if snapshot, ok := st.Snapshots[credential.SnapshotID]; ok {
						credential.RevisionNo = snapshot.RevisionNo
						st.Credentials[no] = credential
					}
				}
			}
			for dossierID, snapshots := range st.Prechecks {
				for i := range snapshots {
					if snapshots[i].RevisionNo != 0 {
						continue
					}
					for _, revision := range st.Revisions[dossierID] {
						if !revision.CreatedAt.After(snapshots[i].CreatedAt) {
							snapshots[i].RevisionNo = revision.Number
						}
					}
				}
				st.Prechecks[dossierID] = snapshots
			}
			st.SchemaVersion = CurrentSchemaVersion
		}
		if st.SchemaVersion > CurrentSchemaVersion {
			return ConstraintError{"schemaVersion", "future"}
		}
		return nil
	})
}
