package backend

type Backend interface {
	Get(string) interface{}
	Put(string, interface{})
}
