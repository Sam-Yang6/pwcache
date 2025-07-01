package pwcache

import (
	"errors"

	"github.com/sarchlab/akita/v3/mem/vm"
)

type pwqueueentry struct {
	req        *vm.TranslationReq
	cyclesleft int //记录该翻译请求在pwqueue中剩余的周期数
	hitlevel   int //记录该翻译请求在pwcache中命中的层数，0代表miss，取值范围为[0,3]
}

func newpwqueueentry(req *vm.TranslationReq, hitlevel int) *pwqueueentry {
	p := new(pwqueueentry)
	p.req = req
	p.hitlevel = hitlevel
	p.cyclesleft = 10
	return p
}

// PWQueue 是一个先进先出队列
type PWQueue struct {
	elements []*pwqueueentry
	capacity int
}

// NewPWQueue 创建一个新的FIFO
func NewPWQueue(capacity int) *PWQueue {
	p := new(PWQueue)
	p.capacity = capacity
	return p
}

// Enqueue 向队列尾部添加一个元素
func (q *PWQueue) Enqueue(element *pwqueueentry) error {
	if len(q.elements) >= q.capacity {
		return errors.New("queue is full")
	}
	q.elements = append(q.elements, element)
	return nil
}

// Dequeue 从队列头部移除一个元素并返回它
func (q *PWQueue) Dequeue() (*pwqueueentry, error) {
	if len(q.elements) == 0 {
		return nil, errors.New("queue is empty")
	}
	element := q.elements[0]
	q.elements = q.elements[1:]
	return element, nil
}

// IsEmpty 检查队列是否为空
func (q *PWQueue) IsEmpty() bool {
	return len(q.elements) == 0
}

// IsFull 检查队列是否已满
func (q *PWQueue) IsFull() bool {
	return len(q.elements) == q.capacity
}

// Size 返回队列的当前大小
func (q *PWQueue) Size() int {
	return len(q.elements)
}

// Capacity 返回队列的容量
func (q *PWQueue) Capacity() int {
	return q.capacity
}

// Update 修改队列中第i个元素的命中值
func (q *PWQueue) Update(i int, hitlevel int) error {
	if i < 0 || i >= len(q.elements) {
		return errors.New("index out of range")
	}
	q.elements[i].hitlevel = hitlevel
	return nil
}

func (q *PWQueue) Index(i int) (*pwqueueentry, error) {
	if i < 0 || i >= len(q.elements) {
		return nil, errors.New("index out of range")
	}
	return q.elements[i], nil
}
