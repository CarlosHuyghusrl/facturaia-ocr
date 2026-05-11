package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/sashabaranov/go-openai"
)

// ErrAllProvidersFailed is returned when all providers in the fallback chain have exhausted
var ErrAllProvidersFailed = errors.New("all AI providers failed")

// isCooldownError detects transient errors (rate limit, quota, service unavailable)
func isCooldownError(err error) bool {
	msg := strings.ToLower(err.Error())
	keywords := []string{
		"cooldown", "quota", "rate_limit", "rate limit",
		"429", "too many requests", "resource exhausted",
		"model_overloaded", "overloaded",
		"503", "502", "service unavailable", "temporarily unavailable",
	}
	for _, kw := range keywords {
		if strings.Contains(msg, kw) {
			return true
		}
	}
	return false
}

// namedProvider wraps a Provider with a display name for logging
type namedProvider struct {
	name     string
	provider Provider
}

// NamedProvider wraps a provider with a name
func NamedProvider(name string, p Provider) namedProvider {
	return namedProvider{name: name, provider: p}
}

// FallbackProvider tries multiple providers in order.
// Falls back on transient errors (rate limit, quota, service unavailable).
// On non-transient errors, bubbles up immediately.
// Returns ErrAllProvidersFailed (wrapping last error) when all providers fail transiently.
type FallbackProvider struct {
	providers []namedProvider
}

// NewFallbackProvider creates a FallbackProvider from an ordered list of named providers
func NewFallbackProvider(providers ...namedProvider) *FallbackProvider {
	return &FallbackProvider{providers: providers}
}

// ExtractData tries each provider in order
func (f *FallbackProvider) ExtractData(prompt string, imageBase64 string) (string, error) {
	var lastErr error
	for _, np := range f.providers {
		result, err := np.provider.ExtractData(prompt, imageBase64)
		if err == nil {
			if len(f.providers) > 1 {
				slog.Info("FallbackProvider success",
					slog.String("provider", np.name),
				)
			}
			return result, nil
		}
		lastErr = err
		slog.Warn("FallbackProvider provider failed",
			slog.String("provider", np.name),
			slog.String("error", err.Error()),
		)
		if !isCooldownError(err) {
			// Non-transient error — don't try next provider, return immediately
			return "", err
		}
		slog.Warn("FallbackProvider transient error — trying next provider",
			slog.String("provider", np.name),
		)
	}
	return "", fmt.Errorf("%w: last error: %v", ErrAllProvidersFailed, lastErr)
}

// Provider interface for AI providers
type Provider interface {
	ExtractData(prompt string, imageBase64 string) (string, error)
}

// OpenAIProvider implements Provider for OpenAI/Azure OpenAI
type OpenAIProvider struct {
	apiKey  string
	baseURL string
	model   string
}

// NewOpenAIProvider creates a new OpenAI provider
func NewOpenAIProvider(apiKey, baseURL, model string) *OpenAIProvider {
	if model == "" {
		model = openai.GPT4 // Default model
	}
	return &OpenAIProvider{
		apiKey:  apiKey,
		baseURL: baseURL,
		model:   model,
	}
}

// ExtractData sends prompt and image to OpenAI
func (p *OpenAIProvider) ExtractData(prompt string, imageBase64 string) (string, error) {
	var config openai.ClientConfig

	// Check if Azure OpenAI
	if strings.Contains(p.baseURL, "azure") {
		config = openai.DefaultAzureConfig(p.apiKey, p.baseURL)
	} else {
		config = openai.DefaultConfig(p.apiKey)
		if p.baseURL != "" {
			config.BaseURL = p.baseURL
		}
	}

	_ = config // Kept compiled; actual call uses p.apiKey/p.baseURL via HTTP directly below

	// Build messages
	var messages []openai.ChatCompletionMessage

	// System message to enforce JSON output
	systemMsg := openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleSystem,
		Content: "Eres un extractor de datos de facturas. SIEMPRE responde ÚNICAMENTE con JSON válido. NUNCA incluyas texto explicativo, narrativa, ni markdown. Tu respuesta debe comenzar con { y terminar con }. Si no puedes extraer datos, responde con un JSON con campos vacíos.",
	}

	if imageBase64 != "" {
		// Vision model with image
		messages = []openai.ChatCompletionMessage{
			systemMsg,
			{
				Role: openai.ChatMessageRoleUser,
				MultiContent: []openai.ChatMessagePart{
					{
						Type: openai.ChatMessagePartTypeText,
						Text: prompt,
					},
					{
						Type: openai.ChatMessagePartTypeImageURL,
						ImageURL: &openai.ChatMessageImageURL{
							URL:    imageBase64,
							Detail: openai.ImageURLDetailAuto,
						},
					},
				},
			},
		}
	} else {
		// Text-only
		messages = []openai.ChatCompletionMessage{
			systemMsg,
			{
				Role:    openai.ChatMessageRoleUser,
				Content: prompt,
			},
		}
	}

	// Build payload with thinking_budget=0 to eliminate Gemini 2.5 Flash reasoning_tokens.
	// go-openai v1.20.4 ChatCompletionRequest does not expose extra_body, so we marshal
	// the request manually and POST directly to {BaseURL}/chat/completions.
	// Pre/post evidence (gemini-2.5-flash via CLIProxy local, prompt OCR realista, 3 calls):
	//   BEFORE: avg 1.75s reasoning_tokens=231 finish=length (TRUNCATED)
	//   AFTER:  avg 0.75s reasoning_tokens=0   finish=stop   (COMPLETE)  → ~2.3x trivial
	type thinkCfg struct {
		ThinkingBudget int `json:"thinking_budget"`
	}
	type googleExtra struct {
		ThinkingConfig thinkCfg `json:"thinking_config"`
	}
	type extraBodyT struct {
		Google googleExtra `json:"google"`
	}
	type customRequest struct {
		Model       string                         `json:"model"`
		Messages    []openai.ChatCompletionMessage `json:"messages"`
		Temperature float32                        `json:"temperature"`
		ExtraBody   extraBodyT                     `json:"extra_body"`
	}
	bodyJSON, err := json.Marshal(customRequest{
		Model:       p.model,
		Messages:    messages,
		Temperature: 0,
		ExtraBody:   extraBodyT{Google: googleExtra{ThinkingConfig: thinkCfg{ThinkingBudget: 0}}},
	})
	if err != nil {
		return "", fmt.Errorf("marshal OpenAI request: %w", err)
	}
	baseURL := p.baseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	httpReq, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		strings.TrimRight(baseURL, "/")+"/chat/completions", bytes.NewReader(bodyJSON))
	if err != nil {
		return "", fmt.Errorf("create OpenAI HTTP request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpClient := &http.Client{Timeout: 120 * time.Second}
	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("OpenAI API call failed: %w", err)
	}
	defer httpResp.Body.Close()
	respBytes, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return "", fmt.Errorf("read OpenAI response: %w", err)
	}
	if httpResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("OpenAI API error %d: %s", httpResp.StatusCode, string(respBytes))
	}
	var resp openai.ChatCompletionResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return "", fmt.Errorf("parse OpenAI response: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response from OpenAI")
	}

	return resp.Choices[0].Message.Content, nil
}

// GeminiProvider implements Provider for Google Gemini
type GeminiProvider struct {
	apiKey string
	model  string
}

// NewGeminiProvider creates a new Gemini provider
func NewGeminiProvider(apiKey, model string) *GeminiProvider {
	if model == "" {
		model = "gemini-pro" // Default model
	}
	return &GeminiProvider{
		apiKey: apiKey,
		model:  model,
	}
}

// ExtractData sends prompt and image to Gemini via REST API with thinkingBudget=0 (faster)
func (p *GeminiProvider) ExtractData(prompt string, imageBase64 string) (string, error) {
	type inlineData struct {
		MIMEType string `json:"mimeType"`
		Data     string `json:"data"`
	}
	type part struct {
		Text       string      `json:"text,omitempty"`
		InlineData *inlineData `json:"inlineData,omitempty"`
	}
	type content struct {
		Parts []part `json:"parts"`
	}
	type thinkingCfg struct {
		ThinkingBudget int `json:"thinkingBudget"`
	}
	type genCfg struct {
		ResponseMIMEType string      `json:"responseMimeType,omitempty"`
		Temperature      float32     `json:"temperature"`
		ThinkingConfig   thinkingCfg `json:"thinkingConfig"`
	}
	type reqBody struct {
		Contents         []content `json:"contents"`
		GenerationConfig genCfg    `json:"generationConfig"`
	}

	parts := []part{{Text: prompt}}

	if imageBase64 != "" {
		imageData := imageBase64
		if strings.HasPrefix(imageData, "data:image") {
			split := strings.Split(imageData, ",")
			if len(split) > 1 {
				imageData = split[1]
			}
		}
		imageBytes, err := decodeBase64(imageData)
		if err != nil {
			return "", fmt.Errorf("failed to decode image: %w", err)
		}
		mimeType := detectMIMEType(imageBytes)
		parts = append(parts, part{
			InlineData: &inlineData{
				MIMEType: mimeType,
				Data:     base64.StdEncoding.EncodeToString(imageBytes),
			},
		})
	}

	body := reqBody{
		Contents: []content{{Parts: parts}},
		GenerationConfig: genCfg{
			ResponseMIMEType: "application/json",
			Temperature:      0,
			ThinkingConfig:   thinkingCfg{ThinkingBudget: 0},
		},
	}

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("failed to marshal Gemini request: %w", err)
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", p.model, p.apiKey)
	httpReq, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(bodyJSON))
	if err != nil {
		return "", fmt.Errorf("failed to create Gemini HTTP request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{Timeout: 120 * time.Second}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("Gemini API call failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read Gemini response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Gemini API error %d: %s", resp.StatusCode, string(respBytes))
	}

	var parsed struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(respBytes, &parsed); err != nil {
		return "", fmt.Errorf("failed to parse Gemini response: %w", err)
	}
	if len(parsed.Candidates) == 0 || len(parsed.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("no response from Gemini")
	}

	var result string
	for _, p := range parsed.Candidates[0].Content.Parts {
		result += p.Text
	}
	return result, nil
}

// OllamaProvider implements Provider for local Ollama
type OllamaProvider struct {
	baseURL string
	model   string
}

// NewOllamaProvider creates a new Ollama provider
func NewOllamaProvider(baseURL, model string) *OllamaProvider {
	if baseURL == "" {
		baseURL = "http://localhost:11434" // Default Ollama URL
	}
	if model == "" {
		model = "mistral" // Default model
	}
	return &OllamaProvider{
		baseURL: baseURL,
		model:   model,
	}
}

// ExtractData sends prompt and image to Ollama
func (p *OllamaProvider) ExtractData(prompt string, imageBase64 string) (string, error) {
	// Build message
	message := map[string]interface{}{
		"role":    "user",
		"content": prompt,
	}

	// Add image if provided
	if imageBase64 != "" {
		// Remove data URI prefix if present
		if strings.HasPrefix(imageBase64, "data:image") {
			parts := strings.Split(imageBase64, ",")
			if len(parts) > 1 {
				imageBase64 = parts[1]
			}
		}

		message["images"] = []string{imageBase64}
	}

	// Build request body
	body := map[string]interface{}{
		"model":       p.model,
		"messages":    []interface{}{message},
		"temperature": 0,
		"stream":      false,
		"format":      "json",
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make HTTP request
	httpClient := &http.Client{
		Timeout: 120 * time.Second, // Ollama can be slow on CPU
	}

	url := p.baseURL + "/api/chat"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("Ollama API call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyText, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("Ollama returned status %d: %s", resp.StatusCode, string(bodyText))
	}

	// Parse response
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	var responseObj struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}

	err = json.Unmarshal(responseBody, &responseObj)
	if err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	return responseObj.Message.Content, nil
}

// Helper functions

func decodeBase64(s string) ([]byte, error) {
	// Use standard base64 decoding
	return base64.StdEncoding.DecodeString(s)
}

func detectMIMEType(data []byte) string {
	// Simple MIME type detection based on magic bytes
	if len(data) < 4 {
		return "application/octet-stream"
	}

	// JPEG
	if data[0] == 0xFF && data[1] == 0xD8 {
		return "image/jpeg"
	}

	// PNG
	if data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 {
		return "image/png"
	}

	// GIF
	if data[0] == 0x47 && data[1] == 0x49 && data[2] == 0x46 {
		return "image/gif"
	}

	// WebP
	if len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return "image/webp"
	}

	return "image/jpeg" // Default assumption for images
}

