package ckg

import "context"

// NewScanner creates a new scanner and loads ignore files.
func NewScanner(store *Store, root string) *Scanner {
	return NewScannerWithIgnores(store, root, nil)
}

// Scan performs an incremental scan of the workspace.
// Returns a list of file paths that need parsing (new or modified)
// and a list of file paths that should be deleted from the DB.
func (s *Scanner) Scan(ctx context.Context) (toParse []string, toDelete []string, err error) {
	res, err := s.ScanChanges(ctx)
	if err != nil {
		return nil, nil, err
	}
	return res.ToParse, res.ToDelete, nil
}
