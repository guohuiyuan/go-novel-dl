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
	fmt.Fprintf(c.out, "[信息] "+format+"\n", args...)
}

func (c *Console) Warnf(format string, args ...any) {
	fmt.Fprintf(c.out, "[警告] "+format+"\n", args...)
}

func (c *Console) Successf(format string, args ...any) {
	fmt.Fprintf(c.out, "[成功] "+format+"\n", args...)
}

func (c *Console) Errorf(format string, args ...any) {
	fmt.Fprintf(c.err, "[错误] "+format+"\n", args...)
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
	suffix := "[回车=否，输入是]"
	if defaultYes {
		suffix = "[回车=是，输入否]"
	}

	line, err := c.Prompt(prompt + " " + suffix)
	if err != nil {
		return false, err
	}

	if line == "" {
		return defaultYes, nil
	}

	switch strings.ToLower(line) {
	case "y", "yes", "是":
		return true, nil
	case "n", "no", "否":
		return false, nil
	default:
		return false, fmt.Errorf("无法识别的输入：%q", line)
	}
}

func (c *Console) Select(prompt string, options []string) (int, error) {
	if len(options) == 0 {
		return -1, fmt.Errorf("没有可选项")
	}

	if len(options) == 1 {
		c.Infof("%s: %s", prompt, options[0])
		return 0, nil
	}

	fmt.Fprintf(c.out, "%s\n", prompt)
	for idx, option := range options {
		fmt.Fprintf(c.out, "  %d) %s\n", idx+1, option)
	}

	line, err := c.Prompt("输入选择序号")
	if err != nil {
		return -1, err
	}

	choice, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || choice < 1 || choice > len(options) {
		return -1, fmt.Errorf("选择序号必须在 1 到 %d 之间", len(options))
	}

	return choice - 1, nil
}

func (c *Console) SelectMany(prompt string, options []string) ([]int, error) {
	if len(options) == 0 {
		return nil, fmt.Errorf("没有可选项")
	}

	fmt.Fprintf(c.out, "%s\n", prompt)
	for idx, option := range options {
		fmt.Fprintf(c.out, "  %d) %s\n", idx+1, option)
	}

	line, err := c.Prompt("输入逗号分隔的选择序号")
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
			return nil, fmt.Errorf("选择序号 %q 必须在 1 到 %d 之间", part, len(options))
		}

		idx := choice - 1
		if _, ok := seen[idx]; ok {
			continue
		}
		seen[idx] = struct{}{}
		indices = append(indices, idx)
	}

	if len(indices) == 0 {
		return nil, fmt.Errorf("至少需要选择一项")
	}

	return indices, nil
}

func (c *Console) PrintSearchResults(results []model.SearchResult) {
	if len(results) == 0 {
		c.Warnf("没有找到搜索结果")
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
