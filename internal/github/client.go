package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"speak2code/internal/task"
	"time"
)

type Client struct {
	Token, Owner, Repo, BaseURL string
	HTTP                        *http.Client
}

func (c Client) Create(ctx context.Context, branch, diff string) (task.PR, error) {
	if c.Token == "" || c.Owner == "" || c.Repo == "" {
		return task.PR{}, fmt.Errorf("GitHub 配置不完整")
	}
	base := c.BaseURL
	if base == "" {
		base = "https://api.github.com"
	}
	body, _ := json.Marshal(map[string]string{
		"title": "灵矽编码任务 " + branch,
		"head":  branch,
		"base":  "main",
		"body":  "由 speak2code Codex Agent 生成。\n\n```diff\n" + diff + "\n```",
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/repos/"+c.Owner+"/"+c.Repo+"/pulls", bytes.NewReader(body))
	if err != nil {
		return task.PR{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	response, err := client.Do(req)
	if err != nil {
		return task.PR{}, err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		return task.PR{}, fmt.Errorf("GitHub API: %s", response.Status)
	}
	var result struct {
		Number int    `json:"number"`
		URL    string `json:"html_url"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return task.PR{}, err
	}
	return task.PR{Number: result.Number, URL: result.URL, Branch: branch}, nil
}
