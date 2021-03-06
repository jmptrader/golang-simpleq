// Package simpleq provides a super-simple queue backed by Redis
package simpleq

import (
	"github.com/Rafflecopter/golang-simpleq/scripts"
	"github.com/garyburd/redigo/redis"
)

// A super simple redis-backed queue
type Queue struct {
	pool     *redis.Pool
	key      string
	listener *Listener
}

// Create a simpleq
func New(pool *redis.Pool, key string) *Queue {
	return &Queue{
		pool: pool,
		key:  key,
	}
}

// End this queue
func (q *Queue) Close() error {
	if q.listener != nil {
		return q.listener.Close()
	}
	return nil
}

// Push an element onto the queue
func (q *Queue) Push(el []byte) (length int64, err error) {
	return redis.Int64(q.do("LPUSH", q.key, el))
}

// Pop an element off the queue
func (q *Queue) Pop() (el []byte, err error) {
	return redis.Bytes(q.do("RPOP", q.key))
}

// Block and Pop an element off the queue
// Use timeout_secs = 0 to block indefinitely
// On timeout, this DOES return an error because redigo does.
func (q *Queue) BPop(timeout_secs int) (el []byte, err error) {
	res, err := q.do("BRPOP", q.key, timeout_secs)

	if res != nil && err == nil {
		if arr, ok := res.([]interface{}); ok && len(arr) == 2 {
			if bres, ok := arr[1].([]byte); ok {
				return bres, err
			}
		}
	}

	return nil, err
}

// Pull an element out the queue (oldest if more than one)
func (q *Queue) Pull(el []byte) (nRemoved int64, err error) {
	return redis.Int64(q.do("LREM", q.key, -1, el))
}

// Pull an element out of the queue and push it onto another atomically
// Note: This will push the element regardless of the return value from pull
func (q *Queue) PullPipe(q2 *Queue, el []byte) (lengthQ2 int64, err error) {
	conn := q.pool.Get()
	defer conn.Close()

	conn.Send("MULTI")
	conn.Send("LREM", q.key, -1, el)
	conn.Send("LPUSH", q2.key, el)
	res, err := redis.Values(conn.Do("EXEC"))

	if len(res) == 2 {
		if ires, ok := res[1].(int64); ok {
			return ires, err
		}
	}

	return 0, err
}

// Safely pull an element out of the queue and push it onto another atomically
// Returns 0 for non-existance in first queue, or length of second queue
func (q *Queue) SPullPipe(q2 *Queue, el []byte) (result int64, err error) {
	conn := q.pool.Get()
	defer conn.Close()
	return redis.Int64(scripts.SafePullPipe.Do(conn, q.key, q2.key, el))
}

// Pop an element out of a queue and put it in another queue atomically
func (q *Queue) PopPipe(q2 *Queue) (el []byte, err error) {
	return redis.Bytes(q.do("RPOPLPUSH", q.key, q2.key))
}

// Block and Pop an element out of a queue and put it in another queue atomically
// On timeout, this doesn't return an error because redigo doesn't.
func (q *Queue) BPopPipe(q2 *Queue, timeout_secs int) (el []byte, err error) {
	return redis.Bytes(q.do("BRPOPLPUSH", q.key, q2.key, timeout_secs))
}

// Clear the queue of elements
func (q *Queue) Clear() (nRemoved int64, err error) {
	return redis.Int64(q.do("DEL", q.key))
}

// List the elements in the queue
func (q *Queue) List() (elements [][]byte, err error) {
	res, err := redis.Values(q.do("LRANGE", q.key, 0, -1))
	if err != nil {
		return nil, err
	}

	elements = make([][]byte, len(res))
	for i, el := range res {
		elements[i], _ = el.([]byte)
	}
	return elements, nil
}

// Create a listener that calls Pop
func (q *Queue) PopListen() *Listener {
	return q.PopPipeListen(nil)
}

// Create a listener that calls PopPipe
func (q *Queue) PopPipeListen(q2 *Queue) *Listener {
	if q.listener != nil {
		panic("Queue can only have one listener")
	}

	q.listener = NewListener(q, q2)

	go func() {
		<-q.listener.ended
		q.listener = nil
	}()

	return q.listener
}

func (q *Queue) do(cmd string, args ...interface{}) (interface{}, error) {
	conn := q.pool.Get()
	defer conn.Close()
	return conn.Do(cmd, args...)
}
