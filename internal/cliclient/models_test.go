package cliclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestModelsAndSetAgentModel(t *testing.T) {
	var saved []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Error("missing authentication")
		}
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/agents/a":
			w.Write([]byte(`{"agent":{"userId":"owner","model":"p/one"}}`))
		case r.URL.Path == "/api/providers":
			q := r.URL.Query()
			switch q.Get("scope") {
			case "agent":
				if q.Get("scopeId") != "a" {
					t.Error("wrong agent scope")
				}
				w.Write([]byte(`{"providers":[{"name":"p","models":[{"id":"one","name":"One"}]}]}`))
			case "user":
				if q.Get("scopeId") != "owner" {
					t.Error("wrong owner scope")
				}
				w.Write([]byte(`{"providers":[{"name":"p","models":[{"id":"one","name":"Duplicate"},{"id":"two"}]}]}`))
			case "system":
				w.WriteHeader(http.StatusForbidden)
			default:
				t.Error("unexpected scope")
			}
		case r.Method == "PUT" && r.URL.Path == "/api/agents/a":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			value, ok := body["model"]
			if !ok || len(body) != 1 {
				t.Error("must update only model, including empty default")
			}
			saved = append(saved, value)
			w.Write([]byte(`{"agent":{"id":"a"}}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
	}))
	defer server.Close()
	c := New(server.URL, "tok")
	models, err := c.Models(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(models, ModelCatalog{Current: "p/one", Options: []ModelOption{{ID: "p/one", Name: "One"}}}) {
		t.Fatalf("models: %#v", models)
	}
	for _, value := range []string{"p/two", ""} {
		if err := c.SetAgentModel(context.Background(), "a", value); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(saved, []string{"p/two", ""}) {
		t.Fatalf("saved: %v", saved)
	}
}

func TestSetAgentModelReportsFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"not owner"}`))
	}))
	defer server.Close()
	if err := New(server.URL, "tok").SetAgentModel(context.Background(), "a", "p/m"); err == nil {
		t.Fatal("server rejection hidden")
	}
}

func TestModelsRespectWholeProviderOverrides(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/agents/a" {
			w.Write([]byte(`{"agent":{"userId":"owner","model":"user/selected"}}`))
			return
		}
		switch r.URL.Query().Get("scope") {
		case "agent":
			w.Write([]byte(`{"providers":[{"name":"shared","models":[{"id":"agent"},{"id":"agent"}]},{"name":"empty","models":[]}]}`))
		case "user":
			w.Write([]byte(`{"providers":[{"name":"shared","models":[{"id":"hidden-user"}]},{"name":"user","models":[{"id":"selected"}]},{"name":"empty","models":[{"id":"hidden"}]}]}`))
		case "system":
			w.Write([]byte(`{"providers":[{"name":"shared","models":[{"id":"hidden-system"}]},{"name":"user","models":[{"id":"hidden-system"}]},{"name":"system","models":[{"id":"available"},{"id":""}]}]}`))
		default:
			t.Errorf("unexpected request: %s", r.URL)
		}
	}))
	defer server.Close()
	got, err := New(server.URL, "tok").Models(context.Background(), "a")
	want := ModelCatalog{Current: "user/selected", Options: []ModelOption{{ID: "shared/agent"}, {ID: "user/selected"}, {ID: "system/available"}}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("catalog = %#v, %v; want %#v", got, err, want)
	}
}
