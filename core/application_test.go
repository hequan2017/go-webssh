package core

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestLoginAndProtectedAPI(t *testing.T) {
	assets := fstest.MapFS{
		"web/html/index.html": &fstest.MapFile{Data: []byte("<!doctype html><title>bastion</title>")},
		"static/app.css":      &fstest.MapFile{Data: []byte("body{}")},
	}
	cfg := testConfig(t)
	app, err := NewApplication(cfg, assets)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(app.Handler())
	defer server.Close()

	response, err := http.Get(server.URL + "/api/assets")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", response.StatusCode)
	}

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	loginBody, _ := json.Marshal(map[string]string{"username": cfg.AdminUser, "password": cfg.AdminPassword})
	response, err = client.Post(server.URL+"/api/auth/login", "application/json", bytes.NewReader(loginBody))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d", response.StatusCode)
	}

	response, err = client.Get(server.URL + "/api/assets")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || string(bytes.TrimSpace(body)) != "[]" {
		t.Fatalf("assets response = %d %s", response.StatusCode, body)
	}

	response, err = client.Get(server.URL + "/api/sessions")
	if err != nil {
		t.Fatal(err)
	}
	sessionBody, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || string(bytes.TrimSpace(sessionBody)) != "[]" {
		t.Fatalf("sessions response = %d %s", response.StatusCode, sessionBody)
	}
}

func TestAdminCanCreateCredentialAssetAndOperator(t *testing.T) {
	assetsFS := fstest.MapFS{
		"web/html/index.html": &fstest.MapFile{Data: []byte("<!doctype html><title>bastion</title>")},
		"static/app.css":      &fstest.MapFile{Data: []byte("body{}")},
	}
	cfg := testConfig(t)
	app, err := NewApplication(cfg, assetsFS)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(app.Handler())
	defer server.Close()
	client := loggedInClient(t, server.URL, cfg.AdminUser, cfg.AdminPassword)

	credential := postJSON(t, client, server.URL+"/api/credentials", map[string]any{"name": "ops-password", "type": "password", "secret": "ssh-secret", "passphrase": ""})
	credentialID, _ := credential["id"].(string)
	if credentialID == "" || credential["encrypted_secret"] != nil || credential["secret"] != nil {
		t.Fatalf("public credential leaked or missing id: %#v", credential)
	}

	asset := postJSON(t, client, server.URL+"/api/assets", map[string]any{
		"name": "prod-1", "host": "10.0.0.10", "port": 22, "username": "ops", "credential_id": credentialID,
		"group": "prod", "description": "production", "host_key_fingerprint": "", "enabled": true,
	})
	if asset["id"] == "" {
		t.Fatal("asset id is empty")
	}
	operator := postJSON(t, client, server.URL+"/api/users", map[string]any{"username": "operator", "password": "operator-password-1", "role": "operator", "asset_groups": []string{"prod"}})
	if operator["password_hash"] != nil {
		t.Fatal("user response leaked password hash")
	}

	operatorClient := loggedInClient(t, server.URL, "operator", "operator-password-1")
	response, err := operatorClient.Get(server.URL + "/api/assets")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var visible []Asset
	if err := json.NewDecoder(response.Body).Decode(&visible); err != nil {
		t.Fatal(err)
	}
	if len(visible) != 1 || visible[0].Group != "prod" {
		t.Fatalf("visible assets = %#v", visible)
	}
}

func TestChangePasswordInvalidatesOldSession(t *testing.T) {
	assetsFS := fstest.MapFS{
		"web/html/index.html": &fstest.MapFile{Data: []byte("<!doctype html><title>bastion</title>")},
		"static/app.css":      &fstest.MapFile{Data: []byte("body{}")},
	}
	cfg := testConfig(t)
	app, err := NewApplication(cfg, assetsFS)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(app.Handler())
	defer server.Close()
	client := loggedInClient(t, server.URL, cfg.AdminUser, cfg.AdminPassword)
	body, _ := json.Marshal(map[string]string{"current_password": cfg.AdminPassword, "new_password": "new-secure-password"})
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/me/password", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("change password status = %d", response.StatusCode)
	}
	response, err = client.Get(server.URL + "/api/me")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old session status = %d", response.StatusCode)
	}
	_ = loggedInClient(t, server.URL, cfg.AdminUser, "new-secure-password")
}

func loggedInClient(t *testing.T, baseURL, username, password string) *http.Client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	response, err := client.Post(baseURL+"/api/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d", response.StatusCode)
	}
	return client
}

func postJSON(t *testing.T, client *http.Client, url string, value any) map[string]any {
	t.Helper()
	body, _ := json.Marshal(value)
	response, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(response.Body)
		t.Fatalf("POST %s = %d %s", url, response.StatusCode, responseBody)
	}
	var result map[string]any
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}
