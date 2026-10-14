package cliclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// ModelOption identifies a configured provider/model pair.
type ModelOption struct{ ID, Name string }

// ModelCatalog combines the selectable models with the latest agent override.
type ModelCatalog struct {
	Options []ModelOption
	Current string
}

// Models lists the same agent, owner and system provider scopes as the dashboard.
func (c *Client) Models(ctx context.Context, agentID string) (ModelCatalog, error) {
	var detail struct {
		Agent struct {
			UserID string `json:"userId"`
			Model  string `json:"model"`
		} `json:"agent"`
	}
	if err := c.getJSON(ctx, "/api/agents/"+url.PathEscape(agentID), &detail); err != nil {
		return ModelCatalog{}, err
	}
	scopes := []struct{ kind, id string }{{"agent", agentID}}
	if detail.Agent.UserID != "" {
		scopes = append(scopes, struct{ kind, id string }{"user", detail.Agent.UserID})
	}
	scopes = append(scopes, struct{ kind, id string }{"system", ""})
	seen := map[string]bool{}
	seenProviders := map[string]bool{}
	var models []ModelOption
	for _, scope := range scopes {
		query := url.Values{"scope": {scope.kind}, "scopeId": {scope.id}}
		resp, err := c.request(ctx, http.MethodGet, "/api/providers?"+query.Encode(), nil)
		if err != nil {
			return ModelCatalog{}, err
		}
		// Some inherited scopes are not visible to the current identity.
		if resp.StatusCode == http.StatusForbidden {
			resp.Body.Close()
			continue
		}
		if resp.StatusCode != http.StatusOK {
			err := responseError(resp)
			resp.Body.Close()
			return ModelCatalog{}, err
		}
		var payload struct {
			Providers []struct {
				Name   string `json:"name"`
				Models []struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"models"`
			} `json:"providers"`
		}
		err = json.NewDecoder(resp.Body).Decode(&payload)
		resp.Body.Close()
		if err != nil {
			return ModelCatalog{}, err
		}
		for _, provider := range payload.Providers {
			// Runtime replaces a whole same-name provider, including its model list.
			// An empty model list must also shadow the lower-priority provider.
			if seenProviders[provider.Name] {
				continue
			}
			seenProviders[provider.Name] = true
			for _, model := range provider.Models {
				id := provider.Name + "/" + model.ID
				if model.ID == "" || seen[id] {
					continue
				}
				seen[id] = true
				models = append(models, ModelOption{ID: id, Name: model.Name})
			}
		}
	}
	return ModelCatalog{Options: models, Current: detail.Agent.Model}, nil
}

// SetAgentModel persists an override; an empty model restores inheritance.
func (c *Client) SetAgentModel(ctx context.Context, agentID, model string) error {
	resp, err := c.request(ctx, http.MethodPut, "/api/agents/"+url.PathEscape(agentID), map[string]string{"model": model})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return responseError(resp)
	}
	return nil
}
