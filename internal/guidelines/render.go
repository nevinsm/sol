package guidelines

import "strings"

// Render performs variable substitution on a guidelines template.
// Variables are referenced as {{key}} in the template.
// Unknown variables are left as-is.
//
// Substitution is performed in a single left-to-right pass via
// strings.NewReplacer, which means:
//   - the result is deterministic (independent of map iteration order),
//   - substitution values are never re-substituted (a value containing
//     `{{other}}` is emitted verbatim, not interpreted as another marker).
func Render(template string, vars map[string]string) string {
	if len(vars) == 0 {
		return template
	}
	pairs := make([]string, 0, len(vars)*2)
	for k, v := range vars {
		pairs = append(pairs, "{{"+k+"}}", v)
	}
	return strings.NewReplacer(pairs...).Replace(template)
}
