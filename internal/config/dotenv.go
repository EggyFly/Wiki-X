package config

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const maxEnvFileSize = 1 << 20

func loadDotEnv(configPath string) ([]string, error) {
	path := strings.TrimSpace(os.Getenv("ENV_FILE"))
	if path == "" {
		path = filepath.Join(filepath.Dir(configPath), ".env")
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open env file %s: %w", path, err)
	}
	defer file.Close()
	if info, err := file.Stat(); err == nil && info.Size() > maxEnvFileSize {
		return nil, fmt.Errorf("env file %s is larger than 1 MiB", path)
	}

	applied := make([]string, 0)
	fail := func(err error) ([]string, error) {
		restoreEnv(applied)
		return nil, err
	}
	reader := bufio.NewReader(io.LimitReader(file, maxEnvFileSize+1))
	lineNo := 0
	for {
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return fail(fmt.Errorf("read env file %s: %w", path, err))
		}
		if errors.Is(err, io.EOF) && line == "" {
			return applied, nil
		}
		lineNo++
		if lineNo > 20000 {
			return fail(fmt.Errorf("env file %s has too many lines", path))
		}
		line = strings.TrimRight(line, "\r\n")
		if len(line) > 8192 {
			return fail(fmt.Errorf("env file %s line %d is too long", path, lineNo))
		}
		key, value, ok, parseErr := parseEnvLine(line)
		if parseErr != nil {
			return fail(fmt.Errorf("env file %s line %d: %w", path, lineNo, parseErr))
		}
		if ok {
			if _, exists := os.LookupEnv(key); !exists {
				if err := os.Setenv(key, value); err != nil {
					return fail(fmt.Errorf("set env %s: %w", key, err))
				}
				applied = append(applied, key)
			}
		}
		if errors.Is(err, io.EOF) {
			return applied, nil
		}
	}
}

func restoreEnv(keys []string) {
	for _, key := range keys {
		_ = os.Unsetenv(key)
	}
}

func parseEnvLine(line string) (string, string, bool, error) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false, nil
	}
	if strings.HasPrefix(line, "export ") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
	}
	index := strings.IndexByte(line, '=')
	if index <= 0 {
		return "", "", false, errors.New("expected KEY=VALUE")
	}
	key := strings.TrimSpace(line[:index])
	if !validEnvKey(key) {
		return "", "", false, fmt.Errorf("invalid environment key %q", key)
	}
	value := strings.TrimSpace(line[index+1:])
	if strings.HasPrefix(value, `"`) {
		end := closingQuote(value)
		if end < 0 {
			return "", "", false, errors.New("unterminated double quote")
		}
		unquoted, err := strconv.Unquote(value[:end+1])
		if err != nil {
			return "", "", false, err
		}
		if err := checkTrailing(value[end+1:]); err != nil {
			return "", "", false, err
		}
		value = unquoted
	} else if strings.HasPrefix(value, "'") {
		end := strings.IndexByte(value[1:], '\'')
		if end < 0 {
			return "", "", false, errors.New("unterminated single quote")
		}
		end++
		if err := checkTrailing(value[end+1:]); err != nil {
			return "", "", false, err
		}
		value = value[1:end]
	} else {
		value = stripInlineComment(value)
	}
	return key, value, true, nil
}

func closingQuote(value string) int {
	for i := 1; i < len(value); i++ {
		if value[i] == '\\' {
			i++
			continue
		}
		if value[i] == '"' {
			return i
		}
	}
	return -1
}

func checkTrailing(rest string) error {
	rest = strings.TrimSpace(rest)
	if rest == "" || strings.HasPrefix(rest, "#") {
		return nil
	}
	return errors.New("unexpected characters after quoted value")
}

func stripInlineComment(value string) string {
	for i := 0; i < len(value); i++ {
		if value[i] == '#' && (i == 0 || value[i-1] == ' ' || value[i-1] == '\t') {
			return strings.TrimSpace(value[:i])
		}
	}
	return value
}

func validEnvKey(key string) bool {
	if key == "" {
		return false
	}
	for i, r := range key {
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r == '_' || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}
