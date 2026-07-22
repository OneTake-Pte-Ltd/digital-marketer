package kvstore

import (
	"fmt"

	"github.com/mattermost/mattermost/server/public/pluginapi"
	"github.com/pkg/errors"
)

type Client struct {
	client *pluginapi.Client
}

func NewKVStore(client *pluginapi.Client) KVStore {
	return &Client{client: client}
}

func postKey(date string, messageID int64) string {
	return fmt.Sprintf("tg:post:%s:%d", date, messageID)
}

func indexKey(date string) string {
	return fmt.Sprintf("tg:index:%s", date)
}

func runStatusKey(date string) string {
	return fmt.Sprintf("run:status:%s", date)
}

func mediaKey(messageID int64) string {
	return fmt.Sprintf("media:%d", messageID)
}

const webhookSecretKey = "neil:webhook:secret"

func (kv *Client) StorePost(date string, messageID int64, post interface{}) error {
	ok, err := kv.client.KV.Set(postKey(date, messageID), post)
	if err != nil {
		return errors.Wrap(err, "KV.Set post failed")
	}
	if !ok {
		return errors.New("KV.Set post returned false")
	}
	return nil
}

func (kv *Client) GetPost(date string, messageID int64, dest interface{}) error {
	return errors.Wrap(kv.client.KV.Get(postKey(date, messageID), dest), "KV.Get post failed")
}

func (kv *Client) AppendToIndex(date string, messageID int64) error {
	key := indexKey(date)
	for range 5 {
		var existing []int64
		if err := kv.client.KV.Get(key, &existing); err != nil {
			return errors.Wrap(err, "KV.Get index failed")
		}
		updated := append(existing, messageID)
		ok, err := kv.client.KV.Set(key, updated)
		if err != nil {
			return errors.Wrap(err, "KV.Set index failed")
		}
		if ok {
			return nil
		}
	}
	return errors.New("failed to append to index after retries")
}

func (kv *Client) GetIndex(date string) ([]int64, error) {
	var ids []int64
	if err := kv.client.KV.Get(indexKey(date), &ids); err != nil {
		return nil, errors.Wrap(err, "KV.Get index failed")
	}
	return ids, nil
}

func (kv *Client) StoreRunStatus(date string, status interface{}) error {
	ok, err := kv.client.KV.Set(runStatusKey(date), status)
	if err != nil {
		return errors.Wrap(err, "KV.Set run status failed")
	}
	if !ok {
		return errors.New("KV.Set run status returned false")
	}
	return nil
}

func (kv *Client) GetRunStatus(date string, dest interface{}) error {
	return errors.Wrap(kv.client.KV.Get(runStatusKey(date), dest), "KV.Get run status failed")
}

func (kv *Client) StoreMedia(messageID int64, record interface{}) error {
	ok, err := kv.client.KV.Set(mediaKey(messageID), record)
	if err != nil {
		return errors.Wrap(err, "KV.Set media failed")
	}
	if !ok {
		return errors.New("KV.Set media returned false")
	}
	return nil
}

func (kv *Client) GetMedia(messageID int64, dest interface{}) error {
	return errors.Wrap(kv.client.KV.Get(mediaKey(messageID), dest), "KV.Get media failed")
}

func (kv *Client) StoreWebhookSecret(secret string) error {
	ok, err := kv.client.KV.Set(webhookSecretKey, secret)
	if err != nil {
		return errors.Wrap(err, "KV.Set webhook secret failed")
	}
	if !ok {
		return errors.New("KV.Set webhook secret returned false")
	}
	return nil
}

func (kv *Client) GetWebhookSecret() (string, error) {
	var secret string
	if err := kv.client.KV.Get(webhookSecretKey, &secret); err != nil {
		return "", errors.Wrap(err, "KV.Get webhook secret failed")
	}
	return secret, nil
}
