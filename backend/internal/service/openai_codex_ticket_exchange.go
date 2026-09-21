package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

const (
	codexTicketCaptureLimit = 64 << 10
	codexTicketBodyLimit    = 16 << 10
	codexTicketHeaderLimit  = 4 << 10
	codexTicketRedacted     = "[REDACTED]"
	codexTicketBodyOmitted  = "[Body omitted: incomplete, oversized, or unsafe to display]"
)

type CodexTicketHTTPMessage struct {
	Method           string              `json:"method,omitempty"`
	URL              string              `json:"url,omitempty"`
	StatusCode       int                 `json:"status_code,omitempty"`
	Headers          map[string][]string `json:"headers,omitempty"`
	Body             string              `json:"body,omitempty"`
	BodyBytes        int64               `json:"body_bytes"`
	BodyTruncated    bool                `json:"body_truncated,omitempty"`
	HeadersTruncated bool                `json:"headers_truncated,omitempty"`
}

type CodexTicketExchange struct {
	RequestedModel  string                  `json:"requested_model"`
	ReportedModels  []string                `json:"reported_models,omitempty"`
	ModelsTruncated bool                    `json:"models_truncated,omitempty"`
	Request         *CodexTicketHTTPMessage `json:"request,omitempty"`
	Response        *CodexTicketHTTPMessage `json:"response,omitempty"`
}

type codexTicketExchangeCapture struct {
	exchange *CodexTicketExchange
	secrets  []string
}

var codexTicketInlineSecrets = regexp.MustCompile(`(?i)gAAAAA[A-Za-z0-9_=-]*|bearer\s+[A-Za-z0-9._~+/=-]+|sk-[A-Za-z0-9_-]+|eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`)
var codexTicketLabeledSecrets = regexp.MustCompile(`(?i)(?:password|passwd|secret|token|cookie|authorization|api[_-]?key)["']?\s*[:=]`)

func startCodexTicketExchange(in openAICodexTicketProbeInput, req *http.Request) *codexTicketExchangeCapture {
	if in.Attempt == nil || req == nil {
		return nil
	}
	c := &codexTicketExchangeCapture{exchange: &CodexTicketExchange{}}
	c.secrets = append(c.secrets, in.Token, in.State)
	if proxy, err := url.Parse(in.ProxyURL); err == nil {
		c.collectURLSecrets(proxy)
	}
	c.collectURLSecrets(req.URL)
	if in.Account != nil {
		c.collectSecrets(in.Account.Credentials, false)
	}
	c.collectHeaderSecrets(req.Header)
	c.exchange.RequestedModel = c.cleanModel(in.Model)
	c.exchange.ModelsTruncated = len(in.Model) > 160
	if in.State == "" {
		in.Attempt.HarvestExchange = c.exchange
	} else {
		in.Attempt.BusinessExchange = c.exchange
	}
	m := &CodexTicketHTTPMessage{Method: c.redact(req.Method)}
	if len(m.Method) > 32 {
		m.Method = codexTicketRedacted
	}
	if req.URL != nil {
		u := *req.URL
		u.User, u.RawQuery, u.Fragment, u.RawFragment, u.Opaque, u.ForceQuery = nil, "", "", "", "", false
		u.Path, u.RawPath = c.redact(u.Path), ""
		m.URL = c.redact(u.String())
		if len(m.URL) > 2048 {
			m.URL = "[URL omitted: oversized]"
		}
	}
	m.Headers, m.HeadersTruncated = c.headers(req.Header)
	c.exchange.Request = m
	if req.Body == nil || req.Body == http.NoBody {
		return c
	}
	if req.GetBody == nil {
		m.Body, m.BodyTruncated = codexTicketBodyOmitted, true
		return c
	}
	body, err := req.GetBody()
	if err != nil || body == nil {
		m.Body, m.BodyTruncated = codexTicketBodyOmitted, true
		return c
	}
	reader := &codexTicketCaptureBody{ReadCloser: body, owner: c, message: m}
	_, _ = io.Copy(io.Discard, io.LimitReader(reader, codexTicketCaptureLimit+1))
	_ = reader.Close()
	return c
}

func (c *codexTicketExchangeCapture) captureResponse(resp *http.Response) {
	if c == nil || resp == nil {
		return
	}
	c.secrets = append(c.secrets, extractOpenAICodexTurnState(resp.Header))
	c.collectHeaderSecrets(resp.Header)
	c.refreshRedaction()
	m := &CodexTicketHTTPMessage{StatusCode: resp.StatusCode}
	m.Headers, m.HeadersTruncated = c.headers(resp.Header)
	c.exchange.Response = m
	if resp.Body != nil {
		resp.Body = &codexTicketCaptureBody{ReadCloser: resp.Body, owner: c, message: m}
	}
}

func (c *codexTicketExchangeCapture) refreshRedaction() {
	c.exchange.RequestedModel = c.cleanModel(c.exchange.RequestedModel)
	for i, model := range c.exchange.ReportedModels {
		c.exchange.ReportedModels[i] = c.cleanModel(model)
	}
	for _, message := range []*CodexTicketHTTPMessage{c.exchange.Request, c.exchange.Response} {
		if message == nil {
			continue
		}
		message.Method, message.URL = c.redact(message.Method), c.redact(message.URL)
		headers, truncated := c.headers(http.Header(message.Headers))
		message.Headers, message.HeadersTruncated = headers, message.HeadersTruncated || truncated
		if message.Body != "" && !message.BodyTruncated {
			clean, ok := c.body([]byte(message.Body))
			if !ok || len(clean) > codexTicketBodyLimit {
				message.Body, message.BodyTruncated = codexTicketBodyOmitted, true
			} else {
				message.Body = clean
			}
		}
	}
}

func (c *codexTicketExchangeCapture) setReportedModels(models []string, truncated bool) {
	if c == nil {
		return
	}
	c.exchange.ModelsTruncated = c.exchange.ModelsTruncated || truncated || len(models) > 16
	c.exchange.ReportedModels = nil
	for _, model := range models[:min(len(models), 16)] {
		if len(model) > 160 {
			c.exchange.ModelsTruncated = true
		}
		c.exchange.ReportedModels = append(c.exchange.ReportedModels, c.cleanModel(model))
	}
}

func (c *codexTicketExchangeCapture) cleanModel(model string) string {
	model = c.redact(model)
	if len(model) > 160 {
		return "[Model omitted: oversized]"
	}
	return model
}

func (c *codexTicketExchangeCapture) collectSecrets(value any, sensitive bool) {
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			c.collectSecrets(child, sensitive || codexTicketSensitiveKey(key))
		}
	case []any:
		for _, child := range value {
			c.collectSecrets(child, sensitive)
		}
	case []string:
		for _, child := range value {
			c.collectSecrets(child, sensitive)
		}
	case string:
		if sensitive && value != "" {
			c.secrets = append(c.secrets, value)
		}
	case json.Number, int, int64, float64:
		if sensitive {
			c.secrets = append(c.secrets, fmt.Sprint(value))
		}
	}
}

func codexTicketSensitiveKey(key string) bool {
	key = strings.NewReplacer("_", "", "-", "", " ", "", ".", "").Replace(strings.ToLower(key))
	for _, name := range []string{"password", "passwd", "token", "secret", "cookie", "authorization", "credential", "apikey", "privatekey", "accesskey", "turnstate", "sessionid"} {
		if strings.Contains(key, name) {
			return true
		}
	}
	switch key {
	case "state", "accountid", "chatgptaccountid", "userid", "chatgptuserid", "xaccountid", "xuserid", "organizationid", "chatgptorganizationid", "orgid", "projectid", "openaiorganization", "openaiproject", "email":
		return true
	}
	return false
}

func (c *codexTicketExchangeCapture) collectURLSecrets(parsed *url.URL) {
	if parsed == nil || parsed.User == nil {
		return
	}
	c.secrets = append(c.secrets, parsed.User.String(), parsed.User.Username())
	if password, ok := parsed.User.Password(); ok {
		c.secrets = append(c.secrets, password)
	}
}

// 短身份值只匹配完整标识符，避免 user_id=1 破坏 gpt-5.1 等模型诊断。
func redactCodexTicketShortSecret(value, secret string) string {
	var result strings.Builder
	start := 0
	for start < len(value) {
		relative := strings.Index(value[start:], secret)
		if relative < 0 {
			break
		}
		index, end := start+relative, start+relative+len(secret)
		result.WriteString(value[start:index])
		bounded := (index == 0 || !codexTicketIdentifierByte(value[index-1])) && (end == len(value) || !codexTicketIdentifierByte(value[end]))
		if bounded {
			result.WriteString(codexTicketRedacted)
		} else {
			result.WriteString(secret)
		}
		start = end
	}
	result.WriteString(value[start:])
	return result.String()
}

func codexTicketIdentifierByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '-' || b == '_' || b == '.'
}

func (c *codexTicketExchangeCapture) redact(value string) string {
	sort.SliceStable(c.secrets, func(i, j int) bool { return len(c.secrets[i]) > len(c.secrets[j]) })
	for _, secret := range c.secrets {
		if secret != "" {
			if len(secret) < 8 {
				value = redactCodexTicketShortSecret(value, secret)
			} else {
				value = strings.ReplaceAll(value, secret, codexTicketRedacted)
			}
		}
	}
	value = codexTicketInlineSecrets.ReplaceAllString(value, codexTicketRedacted)
	if codexTicketLabeledSecrets.MatchString(value) {
		return codexTicketRedacted
	}
	return value
}

func (c *codexTicketExchangeCapture) collectHeaderSecrets(headers http.Header) {
	for key, values := range headers {
		if !codexTicketSensitiveKey(key) {
			continue
		}
		for _, value := range values {
			if value != "" {
				c.secrets = append(c.secrets, value)
			}
			if strings.Contains(strings.ToLower(key), "cookie") {
				parts := strings.Split(value, ";")
				if strings.EqualFold(key, "Set-Cookie") {
					parts = parts[:1]
				}
				for _, part := range parts {
					_, secret, found := strings.Cut(strings.TrimSpace(part), "=")
					if found && secret != "" {
						c.secrets = append(c.secrets, secret)
					}
				}
			}
			if strings.Contains(strings.ToLower(key), "authorization") {
				if _, secret, found := strings.Cut(value, " "); found {
					c.secrets = append(c.secrets, secret)
				}
			}
		}
	}
}

func (c *codexTicketExchangeCapture) headers(headers http.Header) (map[string][]string, bool) {
	result, keys := make(map[string][]string), make([]string, 0, len(headers))
	truncated := false
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		name, values := c.redact(key), []string{codexTicketRedacted}
		switch strings.ToLower(key) {
		case "content-type", "content-length", "date", "server", "retry-after", "request-id", "x-request-id", "cf-ray", "openai-processing-ms":
			values = make([]string, 0, min(len(headers[key]), 16))
			for _, value := range headers[key][:min(len(headers[key]), 16)] {
				if len(value) > codexTicketHeaderLimit {
					value, truncated = codexTicketRedacted, true
				}
				values = append(values, c.redact(value))
			}
		}
		if len(name) > 256 {
			return result, true
		}
		result[name] = values
		encoded, _ := json.Marshal(result)
		if len(encoded) > codexTicketHeaderLimit {
			delete(result, name)
			return result, true
		}
		if len(headers[key]) > 16 {
			return result, true
		}
	}
	return result, truncated
}

type codexTicketCaptureBody struct {
	io.ReadCloser
	owner                       *codexTicketExchangeCapture
	message                     *CodexTicketHTTPMessage
	body                        []byte
	complete, oversized, closed bool
}

func (b *codexTicketCaptureBody) Read(dst []byte) (int, error) {
	n, err := b.ReadCloser.Read(dst)
	b.message.BodyBytes += int64(n)
	if n > codexTicketCaptureLimit-len(b.body) {
		b.oversized = true
	}
	if n > 0 && b.body == nil {
		b.body = make([]byte, 0, codexTicketCaptureLimit)
	}
	b.body = append(b.body, dst[:min(n, codexTicketCaptureLimit-len(b.body))]...)
	if err == io.EOF {
		b.complete = true
	}
	return n, err
}

func (b *codexTicketCaptureBody) Close() error {
	if b.closed {
		return nil
	}
	b.closed = true
	err := b.ReadCloser.Close()
	if !b.complete || b.oversized {
		b.message.Body, b.message.BodyTruncated = codexTicketBodyOmitted, true
	} else if len(b.body) > 0 {
		clean, ok := b.owner.body(b.body)
		if !ok || len(clean) > codexTicketBodyLimit {
			b.message.Body, b.message.BodyTruncated = codexTicketBodyOmitted, true
		} else {
			b.message.Body = clean
		}
	}
	b.body = nil
	b.owner.refreshRedaction()
	return err
}

func (c *codexTicketExchangeCapture) body(raw []byte) (string, bool) {
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return c.jsonBody(trimmed)
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	c.collectSSESecrets(text)
	if !strings.HasSuffix(text, "\n\n") {
		return "", false
	}
	var output strings.Builder
	for _, event := range strings.Split(strings.TrimSuffix(text, "\n\n"), "\n\n") {
		lines, data := []string{}, []string{}
		for _, line := range strings.Split(event, "\n") {
			switch {
			case strings.HasPrefix(line, "data:"):
				data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			case line == "", strings.HasPrefix(line, ":"), strings.HasPrefix(line, "event:"), strings.HasPrefix(line, "id:"), strings.HasPrefix(line, "retry:"):
				lines = append(lines, c.redact(line))
			default:
				return "", false
			}
		}
		if len(data) > 0 {
			clean := strings.Join(data, "\n")
			if clean != "[DONE]" {
				var ok bool
				clean, ok = c.jsonBody(clean)
				if !ok {
					return "", false
				}
			}
			lines = append(lines, "data: "+clean)
		}
		output.WriteString(strings.Join(lines, "\n") + "\n\n")
		if output.Len() > codexTicketBodyLimit {
			return "", false
		}
	}
	return output.String(), true
}

func (c *codexTicketExchangeCapture) jsonBody(raw string) (string, bool) {
	value, ok := codexTicketDecodeJSON(raw)
	if !ok {
		return "", false
	}
	c.collectSecrets(value, false)
	encoded, err := json.Marshal(c.cleanJSON(value, 0))
	return string(encoded), err == nil
}

func codexTicketDecodeJSON(raw string) (any, bool) {
	if !json.Valid([]byte(raw)) {
		return nil, false
	}
	var value any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	err := decoder.Decode(&value)
	return value, err == nil
}

func (c *codexTicketExchangeCapture) collectSSESecrets(text string) {
	for _, event := range strings.Split(text, "\n\n") {
		var data []string
		for _, line := range strings.Split(event, "\n") {
			if strings.HasPrefix(line, "data:") {
				data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			}
		}
		if value, ok := codexTicketDecodeJSON(strings.Join(data, "\n")); ok {
			c.collectSecrets(value, false)
		}
	}
}

func (c *codexTicketExchangeCapture) cleanJSON(value any, depth int) any {
	if depth > 64 {
		return codexTicketRedacted
	}
	switch value := value.(type) {
	case map[string]any:
		clean := make(map[string]any, len(value))
		for key, child := range value {
			if codexTicketSensitiveKey(key) {
				clean[c.redact(key)] = codexTicketRedacted
			} else {
				clean[c.redact(key)] = c.cleanJSON(child, depth+1)
			}
		}
		return clean
	case []any:
		for i, child := range value {
			value[i] = c.cleanJSON(child, depth+1)
		}
		return value
	case string:
		return c.redact(value)
	case json.Number:
		for _, secret := range c.secrets {
			if value.String() == secret {
				return codexTicketRedacted
			}
		}
		return value
	default:
		return value
	}
}
