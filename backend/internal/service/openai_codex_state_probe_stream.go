package service

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

// 完整 SSE 终态已包含验证结论，无需等待上游关闭长连接。
// 保留全部已读事件，交给原验证器检查此前错误和成功终态是否合法。
func readOpenAICodexStateProbeStream(body io.Reader) ([]byte, error) {
	reader := bufio.NewReader(io.LimitReader(body, openAICodexStateProbeMaxBody+1))
	var stream bytes.Buffer
	var event []string
	for {
		line, err := reader.ReadString('\n')
		stream.WriteString(line)
		if stream.Len() > openAICodexStateProbeMaxBody {
			return stream.Bytes(), nil
		}
		text := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if text == "" && strings.HasSuffix(line, "\n") {
			if openAICodexStateProbeTerminal(strings.Join(event, "\n")) {
				return stream.Bytes(), nil
			}
			event = nil
		} else if strings.HasPrefix(text, "data:") {
			event = append(event, strings.TrimPrefix(strings.TrimPrefix(text, "data:"), " "))
		}
		if err != nil {
			if err == io.EOF {
				return stream.Bytes(), nil
			}
			return stream.Bytes(), err
		}
	}
}

func openAICodexStateProbeTerminal(payload string) bool {
	if strings.TrimSpace(payload) == "[DONE]" {
		return true
	}
	var event struct {
		Type     string          `json:"type"`
		Status   string          `json:"status"`
		Error    json.RawMessage `json:"error"`
		Response struct {
			Status string `json:"status"`
		} `json:"response"`
	}
	if json.Unmarshal([]byte(payload), &event) != nil {
		return false
	}
	switch event.Type {
	case "response.completed", "response.failed", "response.incomplete", "error":
		return true
	}
	return event.Status == "failed" || event.Status == "incomplete" || event.Response.Status == "failed" || event.Response.Status == "incomplete" || (len(event.Error) > 0 && string(event.Error) != "null")
}
