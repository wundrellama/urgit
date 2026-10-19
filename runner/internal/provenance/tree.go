package provenance

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ReadTreeFile bounds metadata reads to the checkout at the actual open.
// A source-controlled parent symlink must not expose resolver-host files.
func ReadTreeFile(tree, name string) ([]byte, error) {
	if !filepath.IsLocal(name) {
		return nil, fmt.Errorf("metadata path leaves the source tree: %s", name)
	}
	root, err := os.OpenRoot(tree)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	st, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("symlinked or nonregular metadata is refused: %s", name)
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxActionYAML+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxActionYAML {
		return nil, fmt.Errorf("%w: metadata file %s exceeds %d bytes", ErrBound, name, maxActionYAML)
	}
	return data, nil
}

// WorkflowFiles shares the same confined directory open with the resolver's
// walker and compilers. Filenames and metadata always come from this tree.
func WorkflowFiles(tree string) ([]string, error) {
	root, err := os.OpenRoot(tree)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.Open(".github/workflows")
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	files := []string{}
	for _, e := range entries {
		if !e.IsDir() && (strings.HasSuffix(e.Name(), ".yml") || strings.HasSuffix(e.Name(), ".yaml")) {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	return files, nil
}
