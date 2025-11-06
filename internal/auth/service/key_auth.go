package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/auth/dto"
	"github.com/example/llm-gateway/internal/auth/infra"
	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"
)

type KeyAuthService struct {
	client *http.Client
	rdb    *redis.Client
	base   string
	token  string
	sf     singleflight.Group
}

func NewKeyAuthService(client *http.Client, rdb *redis.Client, baseURL, internalToken string) *KeyAuthService {
	return &KeyAuthService{client: client, rdb: rdb, base: strings.TrimRight(baseURL, "/"), token: internalToken}
}

func (s *KeyAuthService) VerifyKey(ctx context.Context, key string) (*dto.VerifyKeyResponse, error) {
	if s.client == nil || s.base == "" {
		return nil, errors.New("verify client unavailable")
	}
	return infra.PostJSON[dto.VerifyKeyResponse](ctx, s.client, s.base+"/internal/v1/keys/verify", s.token, dto.VerifyKeyRequest{Key: key})
}

func (s *KeyAuthService) ResolveKey(ctx context.Context, keyRaw string) (*dto.VerifyKeyResponse, error) {
	hash := sha256.Sum256([]byte(keyRaw))
	keyHash := hex.EncodeToString(hash[:])
	cacheKey := "key:auth:verify:" + keyHash

	cached, err := s.rdb.Get(ctx, cacheKey).Result()
	if err == nil {
		var resp dto.VerifyKeyResponse
		if err := json.Unmarshal([]byte(cached), &resp); err == nil {
			return &resp, nil
		}
	}

	v, err, _ := s.sf.Do(cacheKey, func() (interface{}, error) {
		cached2, err2 := s.rdb.Get(ctx, cacheKey).Result()
		if err2 == nil {
			var resp2 dto.VerifyKeyResponse
			if err3 := json.Unmarshal([]byte(cached2), &resp2); err3 == nil {
				return &resp2, nil
			}
		}

		verified, err2 := s.VerifyKey(ctx, keyRaw)
		if err2 != nil {
			return nil, err2
		}

		data, err3 := json.Marshal(verified)
		if err3 == nil {
			_ = s.rdb.Set(ctx, cacheKey, string(data), 60*time.Second).Err()
		}

		return verified, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*dto.VerifyKeyResponse), nil
}
