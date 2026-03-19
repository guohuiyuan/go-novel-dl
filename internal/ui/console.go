package ui

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/guohuiyuan/go-novel-dl/internal/model"
)

type Console struct {
	in  *bufio.Reader
	out io.Writer
	err io.Writer
}

func NewConsole(in io.Reader, out io.Writer, err io.Writer) *Console {
	return &Console{
		in:  bufio.NewReader(in),
		out: out,
		err: err,
	}
}

func (c *Console) Infof(format string, args ...any) {
	fmt.Fprintf(c.out, "[INFO] "+format+"\n", args...)
}

func (c *Console) Warnf(format string, args ...any) {
	fmt.Fprintf(c.out, "[WARN] "+format+"\n", args...)
}

func (c *Console) Successf(format string, args ...any) {
	fmt.Fprintf(c.out, "[OK] "+format+"\n", args...)
}

func (c *Console) Errorf(format string, args ...any) {
	fmt.Fprintf(c.err, "[ERROR] "+format+"\n", args...)
}

func (c *Console) Prompt(prompt string) (string, error) {
	if _, err := fmt.Fprintf(c.out, "%s: ", prompt); err != nil {
		return "", err
	}

	line, err := c.in.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}

	return strings.TrimSpace(line), nil
}

func (c *Console) Confirm(prompt string, defaultYes bool) (bool, error) {
	suffix := "[y/N]"
	if defaultYes {
		suffix = "[Y/n]"
	}

	line, err := c.Prompt(prompt + " " + suffix)
	if err != nil {
		return false, err
	}

	if line == "" {
		return defaultYes, nil
	}

	switch strings.ToLower(line) {
	case "y", "yes":
		return true, nil
	case "n", "no":
		return false, nil
	default:
		return false, fmt.Errorf("invalid answer %q", line)
	}
}

func (c *Console) Select(prompt string, options []string) (int, error) {
	if len(options) == 0 {
		return -1, fmt.Errorf("no options available")
	}

	if len(options) == 1 {
		c.Infof("%s: %s", prompt, options[0])
		return 0, nil
	}

	fmt.Fprintf(c.out, "%s\n", prompt)
	for idx, option := range options {
		fmt.Fprintf(c.out, "  %d) %s\n", idx+1, option)
	}

	line, err := c.Prompt("Enter selection number")
	if err != nil {
		return -1, err
	}

	choice, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || choice < 1 || choice > len(options) {
		return -1, fmt.Errorf("selection must be between 1 and %d", len(options))
	}

	return choice - 1, nil
}

func (c *Console) SelectMany(prompt string, options []string) ([]int, error) {
	if len(options) == 0 {
		return nil, fmt.Errorf("no options available")
	}

	fmt.Fprintf(c.out, "%s\n", prompt)
	for idx, option := range options {
		fmt.Fprintf(c.out, "  %d) %s\n", idx+1, option)
	}

	line, err := c.Prompt("Enter comma-separated selection numbers")
	if err != nil {
		return nil, err
	}

	parts := strings.Split(line, ",")
	seen := make(map[int]struct{}, len(parts))
	indices := make([]int, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		choice, err := strconv.Atoi(part)
		if err != nil || choice < 1 || choice > len(options) {
			return nil, fmt.Errorf("selection %q must be between 1 and %d", part, len(options))
		}

		idx := choice - 1
		if _, ok := seen[idx]; ok {
			continue
		}
		seen[idx] = struct{}{}
		indices = append(indices, idx)
	}

	if len(indices) == 0 {
		return nil, fmt.Errorf("at least one selection is required")
	}

	return indices, nil
}

func (c *Console) PrintSearchResults(results []model.SearchResult) {
	if len(results) == 0 {
		c.Warnf("No search results found")
		return
	}

	for idx, result := range results {
		fmt.Fprintf(c.out, "%d. [%s] %s (%s) - %s\n", idx+1, result.Site, result.Title, result.BookID, result.Author)
		if result.Description != "" {
			fmt.Fprintf(c.out, "   %s\n", result.Description)
		}
		if result.URL != "" {
			fmt.Fprintf(c.out, "   %s\n", result.URL)
		}
	}
}
