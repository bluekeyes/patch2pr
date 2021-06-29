package memory

type KVStore struct {
	data map[string]interface{}
}

func NewKVStore() *KVStore {
	return &KVStore{data: make(map[string]interface{})}
}

func (kv *KVStore) Get(key string) interface{} {
	return kv.data[key]
}

func (kv *KVStore) Put(key string, value interface{}) {
	kv.data[key] = value
}
