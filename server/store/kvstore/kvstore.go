package kvstore

// KVStore defines all persistence operations Neil requires.
type KVStore interface {
	// Post storage
	StorePost(date string, messageID int64, post interface{}) error
	GetPost(date string, messageID int64, dest interface{}) error

	// Day index: ordered list of message IDs for a given date
	AppendToIndex(date string, messageID int64) error
	GetIndex(date string) ([]int64, error)

	// Pipeline run status
	StoreRunStatus(date string, status interface{}) error
	GetRunStatus(date string, dest interface{}) error

	// Media record (BunnyCDN URL + transcription)
	StoreMedia(messageID int64, record interface{}) error
	GetMedia(messageID int64, dest interface{}) error

	// Webhook secret (generated once at activation)
	StoreWebhookSecret(secret string) error
	GetWebhookSecret() (string, error)
}
