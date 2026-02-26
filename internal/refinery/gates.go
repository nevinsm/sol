package refinery

import (
	"bufio"
	"os"
	"strings"
)

// LoadQualityGates reads quality gate commands from the given file path.
// If the file does not exist, returns the default gates (no error).
// Lines starting with "#" and blank lines are skipped.
func LoadQualityGates(path string, defaults []string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return defaults, nil
		}
		return nil, err
	}
	defer f.Close()

	var gates []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		gates = append(gates, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(gates) == 0 {
		return defaults, nil
	}
	return gates, nil
}
