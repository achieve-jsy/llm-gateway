package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/example/llm-gateway/internal/auth/config"
	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type OIDCService struct {
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
	oauth    *oauth2.Config
	cfg      config.OIDCConfig
}

func NewOIDCService(cfg config.OIDCConfig) (*OIDCService, error) {
	if cfg.IssuerURL == "" || cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, errors.New("oidc config incomplete")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	provider, err := oidc.NewProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, err
	}
	oauth := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       cfg.Scopes,
	}
	verifier := provider.Verifier(&oidc.Config{ClientID: cfg.ClientID})
	return &OIDCService{provider: provider, verifier: verifier, oauth: oauth, cfg: cfg}, nil
}

func (s *OIDCService) BuildLoginURL(state string) string {
	return s.oauth.AuthCodeURL(state, oauth2.AccessTypeOffline)
}

type OIDCUserInfo struct {
	Sub            string
	Name           string
	Email          string
	DepartmentID   string
	DepartmentName string
	Role           string
}

func (s *OIDCService) HandleCallback(ctx context.Context, code string) (*OIDCUserInfo, error) {
	token, err := s.oauth.Exchange(ctx, code)
	if err != nil {
		return nil, err
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return nil, fmt.Errorf("id_token missing")
	}
	idToken, err := s.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, err
	}
	claims := map[string]any{}
	if err := idToken.Claims(&claims); err != nil {
		return nil, err
	}
	role := s.cfg.DefaultRole
	if raw, ok := claims[s.cfg.RoleClaimPath]; ok {
		switch v := raw.(type) {
		case string:
			for _, item := range s.cfg.AdminMatchValues {
				if v == item {
					role = "admin"
				}
			}
		case []any:
			for _, item := range v {
				for _, admin := range s.cfg.AdminMatchValues {
					if str, ok := item.(string); ok && str == admin {
						role = "admin"
					}
				}
			}
		}
	}
	return &OIDCUserInfo{
		Sub:            stringValue(claims["sub"]),
		Name:           stringValue(claims["name"]),
		Email:          stringValue(claims["email"]),
		DepartmentID:   stringValue(claims["department_id"]),
		DepartmentName: stringValue(claims["department_name"]),
		Role:           role,
	}, nil
}

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func NewContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}

func HTTPStatusForError(err error) int {
	if err == nil {
		return http.StatusOK
	}
	return http.StatusBadGateway
}
