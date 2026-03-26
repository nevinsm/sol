package guidelines

import "strings"

// Render performs variable substitution on a guidelines template.
// Variables are referenced as {{key}} in the template.
// Unknown variables are left as-is.
func Render(template string, vars map[string]string) string {
	result := template
	for k, v := range vars {
		result = strings.ReplaceAll(result, "{{"+k+"}}", v)
	}
	return result
}
