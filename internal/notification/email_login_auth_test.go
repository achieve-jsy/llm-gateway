package notification

import (
	"net/smtp"
	"testing"
)

func TestLoginAuthChallengeResponse(t *testing.T) {
	var auth smtp.Auth = &loginAuth{username: "u@example.com", password: "secret"}
	mech, resp, err := auth.Start(&smtp.ServerInfo{Name: "mail.example.com", TLS: true})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if mech != "LOGIN" || resp != nil {
		t.Fatalf("Start got mech=%q resp=%q", mech, resp)
	}

	got, err := auth.Next([]byte("Username:"), true)
	if err != nil || string(got) != "u@example.com" {
		t.Fatalf("username challenge: got=%q err=%v", got, err)
	}
	got, err = auth.Next([]byte("Password:"), true)
	if err != nil || string(got) != "secret" {
		t.Fatalf("password challenge: got=%q err=%v", got, err)
	}
	got, err = auth.Next(nil, false)
	if err != nil || got != nil {
		t.Fatalf("done: got=%q err=%v", got, err)
	}
}
