package quality

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestProductionImportsRespectPackageOwnership(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate repository")
	}
	root := filepath.Dir(filepath.Dir(file))
	allowed := map[string][]string{
		"frontend":           {},
		"backend/config":     {},
		"backend/hasher":     {},
		"backend/store":      {"backend/config"},
		"backend/blogger":    {"backend/store"},
		"backend/assistant":  {"backend/config"},
		"backend/rss/worker": {"backend/blogger", "backend/config", "backend/hasher"},
		"backend/server":     {"backend/blogger", "frontend"},
	}
	const module = "github.com/rjxby/rss-sum/"
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && path != filepath.Join(root, "backend") && path != filepath.Join(root, "frontend") &&
				!strings.HasPrefix(path, filepath.Join(root, "backend")+string(filepath.Separator)) &&
				!strings.HasPrefix(path, filepath.Join(root, "frontend")+string(filepath.Separator)) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Dir(path) == root || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		folder, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		folder = filepath.ToSlash(folder)
		rules, known := allowed[folder]
		if !known {
			t.Errorf("%s: add an explicit architecture rule for the new production package", folder)
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range parsed.Imports {
			dependency, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if strings.HasPrefix(dependency, "gorm.io/") && folder != "backend/store" {
				t.Errorf("%s: persistence dependency %s belongs in backend/store", path, dependency)
			}
			if !strings.HasPrefix(dependency, module) {
				continue
			}
			local := strings.TrimPrefix(dependency, module)
			permitted := false
			for _, rule := range rules {
				if local == rule {
					permitted = true
				}
			}
			if !permitted {
				t.Errorf("%s: undeclared dependency on %s", path, local)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
