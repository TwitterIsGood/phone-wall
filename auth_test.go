package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthorized(t *testing.T) {
	old := token
	token = "s3cret"
	t.Cleanup(func() { token = old })

	r := httptest.NewRequest("GET", "/", nil)
	if authorized(r) {
		t.Fatal("no credentials should fail")
	}

	r = httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer s3cret")
	if !authorized(r) {
		t.Fatal("bearer should pass")
	}

	r = httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Auth-Token", "s3cret")
	if !authorized(r) {
		t.Fatal("X-Auth-Token should pass")
	}

	r = httptest.NewRequest("GET", "/?token=s3cret", nil)
	if !authorized(r) {
		t.Fatal("query token should pass")
	}

	r = httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: "pw_token", Value: "s3cret"})
	if !authorized(r) {
		t.Fatal("cookie should pass")
	}

	r = httptest.NewRequest("GET", "/?token=wrong", nil)
	if authorized(r) {
		t.Fatal("wrong token should fail")
	}
}

func TestValidateListen(t *testing.T) {
	if err := validateListen("127.0.0.1", ""); err != nil {
		t.Fatalf("loopback without token: %v", err)
	}
	if err := validateListen("::1", ""); err != nil {
		t.Fatalf("IPv6 loopback without token: %v", err)
	}
	if err := validateListen("0.0.0.0", ""); err == nil {
		t.Fatal("LAN bind without token should fail")
	}
	if err := validateListen("0.0.0.0", "tok"); err != nil {
		t.Fatalf("LAN bind with token: %v", err)
	}
}
