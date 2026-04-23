package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

// --- Nano Banana API Integration ---

type BananaRequest struct {
	APIKey      string                 `json:"apiKey"`
	ModelKey    string                 `json:"modelKey"`
	ModelInputs map[string]interface{} `json:"modelInputs"`
}

type BananaResponse struct {
	ID           string   `json:"id"`
	Message      string   `json:"message"`
	Created      int64    `json:"created"`
	APIVersion   string   `json:"apiVersion"`
	ModelOutputs []string `json:"modelOutputs"` // usually base64 strings or URLs
}

func callNanoBananaAPI(prompt string) (string, error) {
	apiKey := os.Getenv("NANO_BANANA_API_KEY")
	modelKey := os.Getenv("NANO_BANANA_MODEL_KEY") // e.g., for SDXL or custom food model
	
	if apiKey == "" || modelKey == "" {
		return "", fmt.Errorf("Nano Banana API credentials not configured in .env")
	}

	reqBody := BananaRequest{
		APIKey:   apiKey,
		ModelKey: modelKey,
		ModelInputs: map[string]interface{}{
			"prompt": prompt,
			"height": 1024,
			"width":  1024,
			"num_inference_steps": 30,
			"guidance_scale":      7.5,
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	// Make request to Banana.dev inference endpoint
	resp, err := http.Post("https://api.banana.dev/start/v4/", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("Nano Banana returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var bananaResp BananaResponse
	if err := json.NewDecoder(resp.Body).Decode(&bananaResp); err != nil {
		return "", err
	}

	if len(bananaResp.ModelOutputs) > 0 {
		return bananaResp.ModelOutputs[0], nil
	}
	
	return "", fmt.Errorf("Nano Banana returned no output")
}


// --- Google Veo 3 / Pro AI Integration ---

type GoogleAIRequest struct {
	Contents []struct {
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	} `json:"contents"`
	GenerationConfig struct {
		Temperature float64 `json:"temperature"`
		MaxTokens   int     `json:"maxOutputTokens"`
	} `json:"generationConfig"`
}

type GoogleAIResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

// callGoogleProAI acts as the Trend Analysis "Brain" to parse trends into viral prompts
func callGoogleProAI(systemContext string, prompt string) (string, error) {
	apiKey := os.Getenv("GOOGLE_PRO_AI_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("GOOGLE_PRO_AI_API_KEY not configured in .env")
	}

	// Utilizing Gemini 1.5 Pro endpoint as part of Google Pro AI Ultra allowance
	apiURL := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/gemini-1.5-pro-latest:generateContent?key=%s", apiKey)

	reqBody := GoogleAIRequest{}
	reqBody.Contents = []struct {
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	}{{
		Parts: []struct {
			Text string `json:"text"`
		}{{Text: systemContext + "\n\n" + prompt}},
	}}
	reqBody.GenerationConfig.Temperature = 0.7
	reqBody.GenerationConfig.MaxTokens = 500

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	resp, err := http.Post(apiURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var aiResp GoogleAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&aiResp); err != nil {
		return "", err
	}

	if len(aiResp.Candidates) > 0 && len(aiResp.Candidates[0].Content.Parts) > 0 {
		return aiResp.Candidates[0].Content.Parts[0].Text, nil
	}

	return "", fmt.Errorf("Google AI returned no content")
}

// callGoogleVeo3 simulates hitting the Veo 3 Video API
// In reality, Veo APIs via Vertex AI require specific Google Auth / Cloud SDK
func callGoogleVeo3(imagePrompt string, motionPrompt string) (string, error) {
	// E.g., vertexai.googleapis.com/v1/projects/.../locations/.../publishers/google/models/veo-3.0
	// As Veo 3 is integrated via Vertex, we use default application credentials (ADC)
	
	// Stubbing the async call to Veo 3 for flow/cinematic generation
	log.Printf("Initiating Google Veo 3 flow-type video generation for prompt: %s (motion: %s)", imagePrompt, motionPrompt)
	
	// Veo rendering usually takes 3-10 minutes. 
	time.Sleep(3 * time.Second) // Simulate network hand-off latency

	// We return a simulated Drive Link or Google Cloud Storage output bucket link
	// Since Google Pro AI Ultra integrates with Drive natively for storage
	mockDriveLink := fmt.Sprintf("https://drive.google.com/file/d/veo3_%d/view?usp=sharing", time.Now().Unix())
	return mockDriveLink, nil
}
