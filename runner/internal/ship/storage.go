package ship

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

// Object describes a completed upload. The ship derives its key from the
// attempt's repository, candidate and trust; the daemon cannot choose them.
type Object struct {
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Upload sends the finished local file using only the ship-signed headers.
// The daemon bearer belongs to the ship and never goes to the object store.
func (c *Client) Upload(ctx context.Context, attempt, name, contentType, path string) (*Object, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, f)
	if err != nil {
		return nil, err
	}
	object := &Object{Size: size, SHA256: hex.EncodeToString(hash.Sum(nil))}
	body, _ := json.Marshal(struct {
		Name        string `json:"name"`
		ContentType string `json:"contentType"`
		*Object
	}{name, contentType, object})
	resp, err := c.do(ctx, http.MethodPost, "/attempt/"+attempt+"/upload", body)
	if err != nil {
		return nil, err
	}
	if resp.Status != http.StatusOK {
		return nil, fmt.Errorf("upload signing: %s", resp.Error())
	}
	var signed struct {
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
	}
	if err := json.Unmarshal(resp.Body, &signed); err != nil {
		return nil, fmt.Errorf("upload signing: %w", err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, signed.URL, f)
	if err != nil {
		return nil, err
	}
	req.ContentLength = size
	for k, v := range signed.Headers {
		req.Header.Set(k, v)
	}
	storeClient := *c.HTTP
	storeClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	stored, err := storeClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer stored.Body.Close()
	if stored.StatusCode < 200 || stored.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(stored.Body, 4096))
		return nil, fmt.Errorf("object store PUT: %d %s", stored.StatusCode, detail)
	}
	return object, nil
}
