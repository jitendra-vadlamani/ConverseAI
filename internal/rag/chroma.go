package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// errCollectionNotFound is returned when a collection doesn't exist yet.
var errCollectionNotFound = errors.New("collection not found")

// chroma is a minimal client for Chroma's v2 HTTP API (Chroma >= 1.0).
type chroma struct {
	base string // .../api/v2/tenants/{t}/databases/{d}
	root string // .../api/v2
	http *http.Client
}

func newChroma(baseURL, tenant, database string) *chroma {
	root := baseURL + "/api/v2"
	return &chroma{
		root: root,
		base: fmt.Sprintf("%s/tenants/%s/databases/%s", root, url.PathEscape(tenant), url.PathEscape(database)),
		http: &http.Client{Timeout: 60 * time.Second},
	}
}

type chromaError struct {
	Status  int
	Message string
}

func (e *chromaError) Error() string {
	return fmt.Sprintf("chroma returned %d: %s", e.Status, e.Message)
}

func (c *chroma) do(ctx context.Context, method, url string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return &chromaError{Status: resp.StatusCode, Message: string(msg)}
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func isNotFound(err error) bool {
	var ce *chromaError
	return errors.As(err, &ce) && ce.Status == http.StatusNotFound
}

func (c *chroma) heartbeat(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, c.root+"/heartbeat", nil, nil)
}

// ensureCollection creates the collection with cosine distance if needed.
func (c *chroma) ensureCollection(ctx context.Context, name string) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	err := c.do(ctx, http.MethodPost, c.base+"/collections", map[string]any{
		"name":          name,
		"metadata":      map[string]any{"hnsw:space": "cosine"},
		"get_or_create": true,
	}, &out)
	return out.ID, err
}

func (c *chroma) collectionID(ctx context.Context, name string) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	err := c.do(ctx, http.MethodGet, c.base+"/collections/"+url.PathEscape(name), nil, &out)
	if isNotFound(err) {
		return "", errCollectionNotFound
	}
	return out.ID, err
}

func (c *chroma) deleteCollection(ctx context.Context, name string) error {
	err := c.do(ctx, http.MethodDelete, c.base+"/collections/"+url.PathEscape(name), nil, nil)
	if isNotFound(err) {
		return nil
	}
	return err
}

func (c *chroma) upsert(ctx context.Context, collectionID string, ids []string, embeddings [][]float64, docs []string, metas []map[string]any) error {
	return c.do(ctx, http.MethodPost, c.base+"/collections/"+collectionID+"/upsert", map[string]any{
		"ids": ids, "embeddings": embeddings, "documents": docs, "metadatas": metas,
	}, nil)
}

type queryResult struct {
	IDs       [][]string         `json:"ids"`
	Documents [][]string         `json:"documents"`
	Distances [][]float64        `json:"distances"`
	Metadatas [][]map[string]any `json:"metadatas"`
}

func (c *chroma) query(ctx context.Context, collectionID string, embedding []float64, n int, where map[string]any) (*queryResult, error) {
	body := map[string]any{
		"query_embeddings": [][]float64{embedding},
		"n_results":        n,
		"include":          []string{"documents", "distances", "metadatas"},
	}
	if where != nil {
		body["where"] = where
	}
	var out queryResult
	if err := c.do(ctx, http.MethodPost, c.base+"/collections/"+collectionID+"/query", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *chroma) count(ctx context.Context, collectionID string, where map[string]any) (int, error) {
	var out struct {
		IDs []string `json:"ids"`
	}
	err := c.do(ctx, http.MethodPost, c.base+"/collections/"+collectionID+"/get", map[string]any{
		"where": where, "limit": 1, "include": []string{},
	}, &out)
	return len(out.IDs), err
}

func (c *chroma) deleteWhere(ctx context.Context, collectionID string, where map[string]any) error {
	return c.do(ctx, http.MethodPost, c.base+"/collections/"+collectionID+"/delete", map[string]any{"where": where}, nil)
}
