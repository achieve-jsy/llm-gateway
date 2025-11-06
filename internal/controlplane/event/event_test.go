package event

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewChangeEvent(t *testing.T) {
	id := uuid.New()
	before := time.Now().UTC().Add(-time.Second)

	ev := NewChangeEvent(ProviderCreated, "provider", id, 123)

	require.NotNil(t, ev)
	assert.Equal(t, ProviderCreated, ev.Type)
	assert.Equal(t, "provider", ev.ResourceType)
	assert.Equal(t, id, ev.ResourceID)
	assert.Equal(t, int64(123), ev.SnapshotVersion)
	assert.False(t, ev.Timestamp.Before(before))
	assert.False(t, ev.Timestamp.After(time.Now().UTC().Add(time.Second)))
}

func TestChangeEventConstants(t *testing.T) {
	cases := map[ChangeEventType]string{
		ProviderCreated:  "provider_created",
		ProviderUpdated:  "provider_updated",
		ProviderDisabled: "provider_disabled",
		ProviderEnabled:  "provider_enabled",
		ProviderDeleted:  "provider_deleted",
		ModelCreated:     "model_created",
		ModelUpdated:     "model_updated",
		ModelDeleted:     "model_deleted",
	}
	for got, want := range cases {
		assert.Equal(t, want, string(got))
	}
}

func TestPublisher_Publish(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	pub := NewPublisher(rdb)
	require.NotNil(t, pub)

	ev := NewChangeEvent(ModelCreated, "model", uuid.New(), 456)

	// 订阅频道以验证消息已发布
	sub := rdb.Subscribe(context.Background(), configChangeChannel)
	defer sub.Close()
	// 等待订阅就绪
	_, err = sub.Receive(context.Background())
	require.NoError(t, err)
	ch := sub.Channel()

	require.NoError(t, pub.Publish(context.Background(), ev))

	select {
	case msg := <-ch:
		var decoded ChangeEvent
		require.NoError(t, json.Unmarshal([]byte(msg.Payload), &decoded))
		assert.Equal(t, ev.Type, decoded.Type)
		assert.Equal(t, ev.ResourceType, decoded.ResourceType)
		assert.Equal(t, ev.ResourceID, decoded.ResourceID)
		assert.Equal(t, ev.SnapshotVersion, decoded.SnapshotVersion)
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive published event in time")
	}
}

func TestPublisher_PublishOnClosedClient(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	addr := mr.Addr()
	mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: addr})
	_ = rdb.Close()

	pub := NewPublisher(rdb)
	err = pub.Publish(context.Background(), NewChangeEvent(ProviderUpdated, "provider", uuid.New(), 0))
	require.Error(t, err)
}
