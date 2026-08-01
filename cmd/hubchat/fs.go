package main

import (
	"io/fs"
	"sort"
)

// readDirNames returns the file names in an fs.FS, sorted.
//
// Migration order is filename order, so sorting is not cosmetic — it is the
// mechanism. Numeric prefixes (0001_, 0002_) sort lexically into the correct
// sequence up to 9999 migrations, at which point the naming scheme is the
// least of anyone's problems.
func readDirNames(fsys fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		names = append(names, entry.Name())
	}

	sort.Strings(names)
	return names, nil
}
