package project

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/estrados/simply-aws/internal/cfn"
)

// ScanTemplates finds and parses all CloudFormation YAML files in dir.
func ScanTemplates(dir string) ([]*cfn.Template, error) {
	var templates []*cfn.Template

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable
		}
		if info.IsDir() {
			// Skip hidden dirs and common non-template dirs
			name := info.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}

		t, err := cfn.ParseFile(path)
		if err != nil {
			return nil // skip unparseable
		}

		// Only include files that look like CF templates
		if t.AWSVersion != "" || len(t.Resources) > 0 {
			rel, _ := filepath.Rel(dir, path)
			t.File = rel
			templates = append(templates, t)
		}

		return nil
	})

	return templates, err
}
