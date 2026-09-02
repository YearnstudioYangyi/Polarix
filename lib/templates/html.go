package templates

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// HTMLTemplate is an HTML template loaded from templates/html. Its ID is the
// filename without the .html extension.
type HTMLTemplate struct {
	Id       string
	Template string
	args     []string
}

var HTMLTemplates []*HTMLTemplate

var htmlTemplateCount uint

// NewHTMLTemplate registers an HTML template. Placeholders use the same
// {{ name }} syntax as Markdown templates.
func NewHTMLTemplate(Id string, Template string) {
	template, args := processTemplate(Template)
	HTMLTemplates = append(HTMLTemplates, &HTMLTemplate{
		Id:       Id,
		Template: template,
		args:     args,
	})
}

func IsHTMLTemplateExist(Id string) bool {
	for _, v := range HTMLTemplates {
		if v.Id == Id {
			return true
		}
	}
	return false
}

// FillHTMLTemplate replaces every placeholder in an HTML template. It returns
// an error when the template does not exist, an argument is missing, or an
// unsupported argument type is supplied.
//
// Values are inserted as provided, matching FillMarkdownTemplate. Escape
// untrusted values before passing them in when the result is sent to a browser.
func FillHTMLTemplate(Id string, arg Args) (string, error) {
	args, err := ToMapString(arg)
	if err != nil {
		return "", err
	}
	for _, v := range HTMLTemplates {
		if v.Id != Id {
			continue
		}

		template := v.Template
		for key, value := range args {
			template = strings.ReplaceAll(template, fmt.Sprintf("{{%v}}", key), value)
		}
		_, missingArgs := processTemplate(template)
		if len(missingArgs) == 0 {
			return template, nil
		}
		return "", fmt.Errorf("Lost args: %s", strings.Join(missingArgs, ", "))
	}
	return "", fmt.Errorf("Template %v not found", Id)
}

// templateDirectory first supports the application's working directory and
// then falls back to the source-tree location. The fallback keeps package tests
// working, where Go changes the current directory to lib/templates.
func templateDirectory(name string) string {
	root := filepath.Join("templates", name)
	if _, err := os.Stat(root); err == nil {
		return root
	}
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		return root
	}
	return filepath.Join(filepath.Dir(sourceFile), "..", "..", "templates", name)
}

func init() {
	root := templateDirectory("html")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".html" {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		NewHTMLTemplate(strings.TrimSuffix(filepath.Base(path), ".html"), string(content))
		htmlTemplateCount++
		return nil
	})
	if err != nil {
		panic(err)
	}
}

func GetHTMLTemplateCount() uint {
	return htmlTemplateCount
}
