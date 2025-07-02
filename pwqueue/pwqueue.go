package pwqueue

import (
	"errors"
	"fmt"

	"github.com/sarchlab/akita/v3/mem/vm"
)

type PWqueueentry struct {
	Req        *vm.TranslationReq
	Cyclesleft int  //记录该翻译请求在pwqueue中剩余的周期数
	Hitlevel   int  //记录该翻译请求在pwcache中命中的层数，0代表miss，取值范围为[0,3]
	Inpwcache  bool //记录该翻译请求是否已经进入pwcache
}

func Newpwqueueentry(req *vm.TranslationReq, hitlevel int) *PWqueueentry {
	p := new(PWqueueentry)
	p.Req = req
	p.Hitlevel = hitlevel
	p.Cyclesleft = 10
	p.Inpwcache = false
	return p
}

// PWQueue 是一个先进先出队列
type PWQueue struct {
	elements []*PWqueueentry
	capacity int
}

// NewPWQueue 创建一个新的FIFO
func NewPWQueue(capacity int) *PWQueue {
	p := new(PWQueue)
	p.capacity = capacity
	return p
}

// Enqueue 向队列尾部添加一个元素
func (q *PWQueue) Enqueue(element *PWqueueentry) error {
	if q.IsFull() {
		return errors.New("queue is full")
	}
	q.elements = append(q.elements, element)
	return nil
}

// Dequeue 从队列头部移除一个元素并返回它
func (q *PWQueue) DequeueAt(i int) (*PWqueueentry, error) {
	if q.IsEmpty() {
		return nil, errors.New("queue is empty")
	}
	if i < 0 || i >= len(q.elements) {
		return nil, fmt.Errorf("index %d out of bounds for queue of length %d", i, len(q.elements))
	}
	element := q.elements[i]
	q.elements = append(q.elements[:i], q.elements[i+1:]...)
	return element, nil
}

// Remove 从队列中移除一个元素
func (q *PWQueue) Remove(PID vm.PID, VA uint64) error {
	for i := 0; i < len(q.elements); i++ {
		if q.elements[i].Req.PID == PID && q.elements[i].Req.VAddr == VA {
			q.DequeueAt(i)
			return nil
		}
	}
	return errors.New("element not found")
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
func (q *PWQueue) Updatehitl(i int, hitlevel int) error {
	if i < 0 || i >= len(q.elements) {
		return errors.New("index out of range")
	}
	q.elements[i].Hitlevel = hitlevel
	return nil
}

func (q *PWQueue) Index(i int) (*PWqueueentry, error) {
	if i < 0 || i >= len(q.elements) {
		return nil, errors.New("index out of range")
	}
	return q.elements[i], nil
}
