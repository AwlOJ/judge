package callback

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"judge-service/internal/store"
)

// Client is responsible for sending results back to the main API server.
type Client struct {
	httpClient *http.Client
	url        string
	secret     string
}

// NewClient creates a new callback client.
func NewClient(url, secret string) *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 10 * time.Second, // 10-second timeout for requests
		},
		url:    url,
		secret: secret,
	}
}

// ResultPayload is the structure of the JSON body sent to the callback endpoint.
type ResultPayload struct {
	SubmissionID string                `json:"submissionId"`
	Result       store.SubmissionResult `json:"result"`
}

// SendResult sends the final judging result to the API server's callback endpoint.
func (c *Client) SendResult(submissionID string, result store.SubmissionResult) error {
	payload := ResultPayload{
		SubmissionID: submissionID,
		Result:       result,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal result payload: %w", err)
	}

	req, err := http.NewRequest("POST", c.url, bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed to create callback request: %w", err)
	}

	// Set required headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-internal-secret", c.secret)

	log.Printf("Sending result for submission %s to callback URL: %s", submissionID, c.url)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send callback request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Callback for submission %s failed with status: %s", submissionID, resp.Status)
		// You might want to read the body here for more error details
		// var errorResponse map[string]interface{}
		// json.NewDecoder(resp.Body).Decode(&errorResponse)
		// log.Printf("Callback error response: %v", errorResponse)
		return fmt.Errorf("callback API returned non-200 status: %s", resp.Status)
	}

	log.Printf("Successfully sent callback for submission %s", submissionID)
	return nil
}
