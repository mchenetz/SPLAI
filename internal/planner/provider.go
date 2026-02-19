package planner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type LLMProvider interface {
	Plan(jobID, reqType, input string) (DAG, error)
}

type HTTPProvider struct {
	endpoint string
	apiKey   string
	client   *http.Client
}

func NewHTTPProvider(endpoint, apiKey string) *HTTPProvider {
	return &HTTPProvider{
		endpoint: strings.TrimSpace(endpoint),
		apiKey:   strings.TrimSpace(apiKey),
		client:   &http.Client{Timeout: 8 * time.Second},
	}
}

type planRequest struct {
	JobID string `json:"job_id"`
	Type  string `json:"type"`
	Input string `json:"input"`
}

type planResponse struct {
	DAGID string `json:"dag_id"`
	Tasks []Task `json:"tasks"`
}

func (p *HTTPProvider) Plan(jobID, reqType, input string) (DAG, error) {
	reqBody, err := json.Marshal(planRequest{JobID: jobID, Type: reqType, Input: input})
	if err != nil {
		return DAG{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return DAG{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return DAG{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return DAG{}, fmt.Errorf("llm planner endpoint returned %s", resp.Status)
	}
	var decoded planResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return DAG{}, err
	}
	if decoded.DAGID == "" {
		decoded.DAGID = jobID + "-dag"
	}
	if len(decoded.Tasks) == 0 {
		return DAG{}, fmt.Errorf("llm planner returned empty task list")
	}
	return DAG{DAGID: decoded.DAGID, Tasks: decoded.Tasks}, nil
}
