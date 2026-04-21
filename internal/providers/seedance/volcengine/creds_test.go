package volcengine

import "testing"

func TestParseCreds_AuthTypeAPIKey(t *testing.T) {
	c := ParseCreds(`{"auth_type":"api_key","api_key":"ark-1","access_key":"AK","secret_key":"SK"}`)
	if c.AuthType != AuthAPIKey || c.APIKey != "ark-1" {
		t.Fatalf("got %#v", c)
	}
	if c.AccessKey != "" || c.SecretKey != "" {
		t.Fatalf("ak/sk must be cleared for api_key mode: %#v", c)
	}
	if !c.HasAuth() {
		t.Fatal("HasAuth")
	}
}

func TestParseCreds_AuthTypeAKSK(t *testing.T) {
	c := ParseCreds(`{"auth_type":"aksk","api_key":"ark-1","access_key":"AK","secret_key":"SK","region":"cn-shanghai"}`)
	if c.AuthType != AuthAKSK || c.AccessKey != "AK" || c.SecretKey != "SK" || c.Region != "cn-shanghai" {
		t.Fatalf("got %#v", c)
	}
	if c.APIKey != "" {
		t.Fatalf("api_key must be cleared for aksk mode: %#v", c)
	}
}

func TestParseCreds_InferAPIKey(t *testing.T) {
	c := ParseCreds(`{"api_key":"uuid-key"}`)
	if c.AuthType != AuthAPIKey || c.APIKey != "uuid-key" || !c.HasAuth() {
		t.Fatalf("got %#v", c)
	}
}

func TestParseCreds_InferAKSK(t *testing.T) {
	c := ParseCreds(`{"access_key":"AK","secret_key":"SK"}`)
	if c.AuthType != AuthAKSK || !c.HasAuth() {
		t.Fatalf("got %#v", c)
	}
}

func TestParseCreds_BareAPIKey(t *testing.T) {
	c := ParseCreds("bare-ark-key")
	if c.AuthType != AuthAPIKey || c.APIKey != "bare-ark-key" {
		t.Fatalf("got %#v", c)
	}
}

func TestParseCreds_MissingRequired(t *testing.T) {
	c := ParseCreds(`{"auth_type":"api_key"}`)
	if c.HasAuth() {
		t.Fatalf("empty api_key should not HasAuth: %#v", c)
	}
	c = ParseCreds(`{"auth_type":"aksk","access_key":"AK"}`)
	if c.HasAuth() {
		t.Fatalf("missing secret should not HasAuth: %#v", c)
	}
}
