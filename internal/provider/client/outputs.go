package client

// func (c *Client) CreateOutput(ctx context.Context, organizationID string, request CreateOutputRequest) (*Output, error) {
// 	endpoint := c.buildURL(fmt.Sprintf("v2/%s/outputs", organizationID))

// 	body, err := json.Marshal(request)
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to marshal request: %w", err)
// 	}

// 	fmt.Printf("DEBUG: Making request to endpoint: %s\n", endpoint)
// 	fmt.Printf("DEBUG: Request body: %s\n", string(body))
// 	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to create request: %w", err)
// 	}

// 	var output Output
// 	if err := c.doRequest(req, &output); err != nil {
// 		return nil, err
// 	}

// 	return &output, nil
// }

// func (c *Client) GetOutput(ctx context.Context, organizationID, outputID string) (*Output, error) {
// 	endpoint := c.buildURL(fmt.Sprintf("v1/%s/outputs/%s", organizationID, outputID))

// 	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to create request: %w", err)
// 	}

// 	var output Output
// 	if err := c.doRequest(req, &output); err != nil {
// 		return nil, err
// 	}

// 	return &output, nil
// }

// func (c *Client) UpdateOutput(ctx context.Context, organizationID, outputID string, request UpdateOutputRequest) (*Output, error) {
// 	endpoint := c.buildURL(fmt.Sprintf("v2/%s/outputs/%s", organizationID, outputID))

// 	body, err := json.Marshal(request)
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to marshal request: %w", err)
// 	}

// 	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, endpoint, bytes.NewReader(body))
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to create request: %w", err)
// 	}

// 	var output Output
// 	if err := c.doRequest(req, &output); err != nil {
// 		return nil, err
// 	}

// 	return &output, nil
// }

// func (c *Client) DeleteOutput(ctx context.Context, organizationID, outputID string) error {
// 	endpoint := c.buildURL(fmt.Sprintf("v1/%s/outputs/%s", organizationID, outputID))

// 	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
// 	if err != nil {
// 		return fmt.Errorf("failed to create request: %w", err)
// 	}

// 	return c.doRequest(req, nil)
// }
