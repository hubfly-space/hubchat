package config

import (
	"bufio"
	"os"
	"strings"
)

// loadDotEnv reads KEY=VALUE lines from path into the process environment,
// skipping any key that is already set.
//
// The real environment always wins. That ordering is what makes a committed
// .env.example safe to copy: an operator who exports HUBCHAT_DATABASE_URL for
// one command does not have to remember whether a file is quietly overriding
// it.
//
// A missing or unreadable file is not an error — the file is optional by
// design, and every value in it has an environment equivalent.
//
// The subset of dotenv syntax supported is deliberately small: comments,
// blank lines, an optional `export ` prefix, and single- or double-quoted
// values. No interpolation, no multi-line values, no command substitution.
// Anything that needs those belongs in a real secret store, not a text file
// next to the binary.
func loadDotEnv(path string) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		key, value, ok := parseDotEnvLine(scanner.Text())
		if !ok {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		_ = os.Setenv(key, value)
	}
}

// parseDotEnvLine splits one line into a key and value. ok is false for
// comments, blanks, and anything that is not a recognisable assignment —
// silently, because a malformed line should not stop the server from booting
// on values that did parse.
func parseDotEnvLine(line string) (key, value string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}

	line = strings.TrimPrefix(line, "export ")

	key, value, found := strings.Cut(line, "=")
	if !found {
		return "", "", false
	}

	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", false
	}

	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		first, last := value[0], value[len(value)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return key, value[1 : len(value)-1], true
		}
	}

	// Unquoted values end at an inline comment, so a trailing `# note` does
	// not become part of a database URL.
	if idx := strings.Index(value, " #"); idx >= 0 {
		value = strings.TrimSpace(value[:idx])
	}

	return key, value, true
}
